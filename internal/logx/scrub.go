package logx

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Users share their log for help, so it shouldn't give away who they are.
// Every line is scrubbed before it is written (Printf), and the whole file
// again when it is copied (ScrubbedLog), which covers crash output and
// lines from older versions. What goes:
//
//   - the user's name in paths: C:\Users\<user>\Music\airhorn.mp3. Sound
//     and folder names stay, so problems can still be traced to a file;
//   - the computer's name, the organisation in OneDrive folders
//     ("OneDrive - <org>") and email addresses.
//
// Names of audio devices and apps stay: they are needed to help, and
// rarely personal.

// scrubber replaces personal details in text.
type scrubber struct {
	profile *regexp.Regexp // the user's own folder names, exactly
	pc      *regexp.Regexp // the computer's name; nil when too short to be safe
}

var (
	reEmail    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	reOneDrive = regexp.MustCompile(`(OneDrive - )[^\\/"'\r\n]+`)
	// reUsers finds any profile folder (other users', short 8.3 names);
	// \\ repeats as in %q-quoted paths.
	reUsers = regexp.MustCompile(`(?i)([\\/]+(?:Users|Documents and Settings)[\\/]+)([^\\/\s"'*?<>|:]+)`)
)

// sharedProfiles are folders under Users that belong to no one.
var sharedProfiles = map[string]bool{"public": true, "default": true, "default user": true, "all users": true, "<user>": true}

// newScrubber scrubs the given account names (the profile folder's name,
// the user name; either may be empty) and computer name.
func newScrubber(names []string, computer string) *scrubber {
	s := &scrubber{}
	var alts []string
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[strings.ToLower(n)] || sharedProfiles[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		alts = append(alts, regexp.QuoteMeta(n))
	}
	if len(alts) > 0 {
		// The name must end at a separator, quote, space or the end: one
		// user's name inside another's ("anna" in "annabel") stays.
		s.profile = regexp.MustCompile(`(?i)([\\/]+(?:Users|Documents and Settings)[\\/]+)(?:` + strings.Join(alts, "|") + `)([\\/"'\s:,;)]|$)`)
	}
	if computer = strings.TrimSpace(computer); len(computer) >= 4 {
		s.pc = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(computer) + `\b`)
	}
	return s
}

func (s *scrubber) scrub(text string) string {
	text = reEmail.ReplaceAllString(text, "<email>")
	if s.profile != nil {
		text = s.profile.ReplaceAllString(text, "${1}<user>${2}")
	}
	text = reUsers.ReplaceAllStringFunc(text, func(m string) string {
		sub := reUsers.FindStringSubmatch(m)
		if sharedProfiles[strings.ToLower(sub[2])] {
			return m
		}
		return sub[1] + "<user>"
	})
	text = reOneDrive.ReplaceAllString(text, "${1}<org>")
	if s.pc != nil {
		text = s.pc.ReplaceAllString(text, "<pc>")
	}
	return text
}

// thisScrubber scrubs this account and computer, read once from Windows'
// environment.
var thisScrubber = sync.OnceValue(func() *scrubber {
	var profile string
	if home := os.Getenv("USERPROFILE"); home != "" {
		profile = filepath.Base(home)
	}
	computer := os.Getenv("COMPUTERNAME")
	if computer == "" {
		computer, _ = os.Hostname()
	}
	return newScrubber([]string{profile, os.Getenv("USERNAME")}, computer)
})

// Scrub removes personal details from text for the log: the user's name in
// paths, the computer's name, OneDrive organisations and email addresses.
func Scrub(text string) string { return thisScrubber().scrub(text) }

// ScrubbedLog returns the current log file, scrubbed, for sharing.
func ScrubbedLog() (string, error) {
	mu.Lock()
	path := logPath
	mu.Unlock()
	if path == "" {
		return "", os.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Scrub(string(data)), nil
}
