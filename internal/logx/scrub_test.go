package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrub(t *testing.T) {
	s := newScrubber([]string{"John Smith", "jsmith"}, "DESKTOP-4F2K9")
	for in, want := range map[string]string{
		// The user's folder, also with spaces, quoted (%q doubles
		// backslashes), with forward slashes and at the end.
		`play C:\Users\John Smith\Music\Sounds\airhorn.mp3`:   `play C:\Users\<user>\Music\Sounds\airhorn.mp3`,
		`folder "C:\Users\john smith\Sounds" added`:           `folder "C:\Users\<user>\Sounds" added`,
		`C:/Users/jsmith/AppData/Local/Resound`:               `C:/Users/<user>/AppData/Local/Resound`,
		`home is C:\Users\jsmith`:                             `home is C:\Users\<user>`,
		`C:\Users\JSMITH~1\AppData`:                           `C:\Users\<user>\AppData`, // short name
		`C:\Users\anna\Music`:                                 `C:\Users\<user>\Music`,   // another account
		`C:\Users\Public\Music\x.wav`:                         `C:\Users\Public\Music\x.wav`,
		`C:\Users\jsmithers\x.wav`:                            `C:\Users\<user>\x.wav`, // not the user's, still a name
		`D:\Sounds\John Smith Remix.mp3`:                      `D:\Sounds\John Smith Remix.mp3`,
		`C:\Users\jsmith\OneDrive - Contoso Ltd\Sounds\a.mp3`: `C:\Users\<user>\OneDrive - <org>\Sounds\a.mp3`,
		`signed in as john.smith@example.com`:                 `signed in as <email>`,
		`host desktop-4f2k9 on 192.168.1.20`:                  `host <pc> on 192.168.1.20`,
		`play #3 "Airhorn" (1.2s) on 2 output(s)`:             `play #3 "Airhorn" (1.2s) on 2 output(s)`,
		`You hear: Headphones (Realtek(R) Audio)`:             `You hear: Headphones (Realtek(R) Audio)`,
	} {
		if got := s.scrub(in); got != want {
			t.Errorf("scrub(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// Short computer names would hit ordinary words; they are left alone.
func TestScrubShortComputerName(t *testing.T) {
	if got := newScrubber(nil, "PC").scrub("PC speakers"); got != "PC speakers" {
		t.Errorf("got %q", got)
	}
}

func TestScrubbedLog(t *testing.T) {
	dir := t.TempDir()
	done, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	home := os.Getenv("USERPROFILE")
	if home == "" {
		t.Skip("no USERPROFILE")
	}
	// A line written around the logger, as a crash would be.
	f, err := os.OpenFile(filepath.Join(dir, "debug.log"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("panic: open " + filepath.Join(home, "x.mp3") + "\n")
	f.Close()
	Printf("folder %q", filepath.Join(home, "Music"))
	got, err := ScrubbedLog()
	if err != nil {
		t.Fatal(err)
	}
	if name := filepath.Base(home); strings.Contains(got, `\`+name+`\`) {
		t.Errorf("the user's name is in the log:\n%s", got)
	}
	if !strings.Contains(got, `\Users\<user>\x.mp3`) || !strings.Contains(got, `\\Users\\<user>\\Music`) {
		t.Errorf("log: %q", got)
	}
}
