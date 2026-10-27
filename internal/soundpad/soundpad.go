// Package soundpad reads Soundpad's sound lists and keys, so the guided
// setup can carry them over. Soundpad keeps sounds from anywhere in lists of
// files (.spl, XML) and groups them into categories freely; Resound's tabs
// are folders. Plan therefore groups a list's sounds by the folder each file
// is in, and the setup offers those folders.
//
// Nothing is trusted: a list is parsed with limits, every path is checked
// on the disk, and keys are taken only where their encoding is known
// (settings.go). Soundpad's files and registry key are only read.
//
// The format is Soundpad 4's (checked with 4.0.35): <Soundlist> holds
// <Sound url title color [key keyModifiers]> elements, then <Categories>
// and <Hotbar> that refer to the sounds by their 0-based position.
// %APPDATA%\Leppsoft\soundlist.spl is the list Soundpad shows (its url
// attribute names the file it was last saved as); other lists are files the
// user saved, found in that folder and among Soundpad's recent lists.
package soundpad

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/motivated2die/resound/internal/audio"
)

// MaxSize is the largest list read.
const MaxSize = 16 << 20

const (
	maxSounds    = 50000 // per list
	maxLists     = 20    // offered from Soundpad's folder and recent lists
	maxCountWalk = 20000 // files looked at when counting a folder's sounds
)

// CurrentList is the list Soundpad shows, in Dir.
const CurrentList = "soundlist.spl"

// Dir is where Soundpad keeps its lists: %APPDATA%\Leppsoft.
func Dir() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Leppsoft")
}

// List is one sound list.
type List struct {
	Path     string
	Current  bool   // Soundpad's current list (CurrentList)
	SavedAs  string // for the current list, the file it was last saved as
	Modified time.Time
	Sounds   []Sound
	// Categories are the user's categories (not Soundpad's built-in "all
	// sounds" one).
	Categories []string
	// Skipped counts unreadable sounds.
	Skipped int
}

// Sound is one sound of a list.
type Sound struct {
	Path   string
	Title  string
	Color  string // the phone's color nearest to Soundpad's, "" for none
	Hotbar bool   // on the hotbar, Soundpad's quick access
	Hotkey bool   // has a hotkey of its own
}

// ErrNotSoundpad is returned for XML that isn't a sound list.
var ErrNotSoundpad = errors.New("not a Soundpad sound list")

type xmlList struct {
	XMLName xml.Name `xml:"Soundlist"`
	URL     string   `xml:"url,attr"`
	Sounds  []struct {
		URL   string `xml:"url,attr"`
		Title string `xml:"title,attr"`
		Color string `xml:"color,attr"`
		Key   string `xml:"key,attr"`
	} `xml:"Sound"`
	Categories []struct {
		Name string `xml:"name,attr"`
		Type string `xml:"type,attr"`
	} `xml:"Categories>Category"`
	Hotbar []struct {
		Items []struct {
			SoundID string `xml:"soundId,attr"`
		} `xml:"HotbarItem"`
	} `xml:"Hotbar>HotbarPage"`
}

// Load reads and parses the list at path.
func Load(path string) (*List, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, fmt.Errorf("%s is larger than %d MB", filepath.Base(path), MaxSize>>20)
	}
	l, err := Parse(data)
	if err != nil {
		return nil, err
	}
	l.Path, l.Modified = path, info.ModTime()
	l.Current = strings.EqualFold(filepath.Base(path), CurrentList)
	if !l.Current {
		l.SavedAs = ""
	}
	return l, nil
}

// Parse reads a sound list. Go's decoder doesn't resolve external entities
// or DTDs, so a list can't make it read anything else.
func Parse(data []byte) (*List, error) {
	var x xmlList
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSoundpad, err)
	}
	l := &List{}
	if x.URL != "" && filepath.IsAbs(x.URL) {
		l.SavedAs = filepath.Clean(x.URL)
	}
	hotbar := map[int]bool{}
	for _, page := range x.Hotbar {
		for _, it := range page.Items {
			if i, err := strconv.Atoi(it.SoundID); err == nil {
				hotbar[i] = true
			}
		}
	}
	for i, s := range x.Sounds {
		if i == maxSounds {
			l.Skipped += len(x.Sounds) - maxSounds
			break
		}
		if s.URL == "" || len(s.URL) >= 1024 || !filepath.IsAbs(s.URL) {
			l.Skipped++ // a stream or a damaged entry
			continue
		}
		key, err := strconv.Atoi(s.Key)
		l.Sounds = append(l.Sounds, Sound{
			Path:   filepath.Clean(s.URL),
			Title:  s.Title,
			Color:  PhoneColor(s.Color),
			Hotbar: hotbar[i],
			Hotkey: err == nil && key > 0,
		})
	}
	for _, c := range x.Categories {
		if c.Type == "" && c.Name != "" {
			l.Categories = append(l.Categories, c.Name)
		}
	}
	return l, nil
}

// Discover lists the sound lists to offer: the current list first, then
// lists in dir and Soundpad's recent lists (recent, from its settings) that
// exist. Soundpad's play statistics (stats.spl) and its lock files ("^…")
// aren't lists.
func Discover(dir string, recent []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = filepath.Clean(p)
		k := strings.ToLower(p)
		base := strings.ToLower(filepath.Base(p))
		if seen[k] || len(out) == maxLists || !filepath.IsAbs(p) || filepath.Ext(base) != ".spl" ||
			base == "stats.spl" || strings.HasPrefix(base, "^") {
			return
		}
		if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
			return
		}
		seen[k] = true
		out = append(out, p)
	}
	if dir != "" {
		add(filepath.Join(dir, CurrentList))
	}
	for _, p := range recent {
		add(p)
	}
	if dir != "" {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !e.IsDir() {
				add(filepath.Join(dir, e.Name()))
			}
		}
	}
	return out
}

// Plan is what an import of some lists would carry over, checked against
// the disk.
type Plan struct {
	// Folders hold the lists' sounds, each with the sounds in its
	// subfolders, in the order the lists first name them.
	Folders []Folder
	// Sounds are the sounds whose files exist and play, once each.
	Sounds []Sound
	// Missing counts sounds whose files are gone; Unplayable those in a
	// format Resound doesn't play (Soundpad plays M4A, WMA, AAC and more).
	Missing, Unplayable int
	// HotkeySounds counts sounds with a hotkey of their own; Categories the
	// lists' categories, by name. Neither carries over.
	HotkeySounds, Categories int
	Skipped                  int
}

// Folder is a folder with sounds of the lists.
type Folder struct {
	Path string
	// Listed counts the lists' sounds in it (and its subfolders); Files
	// all the sounds Resound would find there, Many when there are more
	// than it counted.
	Listed, Files int
	Many          bool
}

// Suggested reports whether the folder holds mostly sounds of the lists,
// so adding it doesn't bring many others. The setup turns such folders on.
func (f Folder) Suggested() bool { return !f.Many && 2*f.Listed >= f.Files }

// Disk is what Plan asks about files; tests replace it.
type Disk struct {
	IsFile func(path string) bool
	// CountSounds counts the playable files in dir and its subfolders, up
	// to limit, and reports whether it stopped there.
	CountSounds func(dir string, limit int) (n int, more bool)
}

// OS is the real disk.
var OS = Disk{
	IsFile: func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && info.Mode().IsRegular()
	},
	CountSounds: func(dir string, limit int) (int, bool) {
		n, seen := 0, 0
		more := false
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if seen++; seen > maxCountWalk || n >= limit {
				more = true
				return filepath.SkipAll
			}
			name := d.Name()
			if d.IsDir() && p != dir && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$")) {
				return filepath.SkipDir // the library skips them too
			}
			if !d.IsDir() && Playable(name) {
				n++
			}
			return nil
		})
		return n, more
	},
}

// Playable reports whether Resound plays the file.
func Playable(path string) bool {
	return slices.Contains(audio.SupportedExtensions, strings.ToLower(filepath.Ext(path)))
}

// PlanLists checks the lists against the disk.
func PlanLists(lists []*List, disk Disk) Plan {
	var p Plan
	seen := map[string]bool{}
	byDir := map[string]int{}
	categories := map[string]bool{}
	for _, l := range lists {
		for _, c := range l.Categories {
			categories[strings.ToLower(c)] = true
		}
		p.Skipped += l.Skipped
		for _, s := range l.Sounds {
			k := strings.ToLower(s.Path)
			if seen[k] {
				continue
			}
			seen[k] = true
			if s.Hotkey {
				p.HotkeySounds++
			}
			switch {
			case !Playable(s.Path):
				p.Unplayable++
				continue
			case !disk.IsFile(s.Path):
				p.Missing++
				continue
			}
			p.Sounds = append(p.Sounds, s)
			dir := filepath.Dir(s.Path)
			i, ok := byDir[strings.ToLower(dir)]
			if !ok {
				i = len(p.Folders)
				byDir[strings.ToLower(dir)] = i
				p.Folders = append(p.Folders, Folder{Path: dir})
			}
			p.Folders[i].Listed++
		}
	}
	// A folder inside another one comes with it.
	var top []Folder
	for i, f := range p.Folders {
		in := -1
		for j, g := range p.Folders {
			if i != j && within(f.Path, g.Path) && (in < 0 || len(g.Path) < len(p.Folders[in].Path)) {
				in = j
			}
		}
		if in < 0 {
			top = append(top, f)
		}
	}
	for i := range top {
		top[i].Listed = 0
		for _, f := range p.Folders {
			if strings.EqualFold(f.Path, top[i].Path) || within(f.Path, top[i].Path) {
				top[i].Listed += f.Listed
			}
		}
		const countLimit = 5000
		top[i].Files, top[i].Many = disk.CountSounds(top[i].Path, countLimit)
		top[i].Files = max(top[i].Files, top[i].Listed)
	}
	p.Folders = top
	p.Categories = len(categories)
	return p
}

// Favorites counts the hotbar's sounds.
func (p *Plan) Favorites() int {
	n := 0
	for _, s := range p.Sounds {
		if s.Hotbar {
			n++
		}
	}
	return n
}

// Colored counts the sounds with a color.
func (p *Plan) Colored() int {
	n := 0
	for _, s := range p.Sounds {
		if s.Color != "" {
			n++
		}
	}
	return n
}

// within reports whether path is inside dir (not dir itself).
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// PhoneColor is the phone's card color (webapp styles.css) nearest to a
// Soundpad color ("RRGGBB"): by hue, grey when it has little color. "" for
// none or a malformed one.
func PhoneColor(hex string) string {
	if len(hex) != 6 {
		return ""
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return ""
	}
	r, g, b := float64(v>>16&0xFF)/255, float64(v>>8&0xFF)/255, float64(v&0xFF)/255
	hi, lo := max(r, g, b), min(r, g, b)
	if hi-lo < 0.15 || hi < 0.12 {
		return "grey" // black, white and greys
	}
	var h float64
	switch d := hi - lo; hi {
	case r:
		h = 60 * (g - b) / d
	case g:
		h = 60 * (2 + (b-r)/d)
	default:
		h = 60 * (4 + (r-g)/d)
	}
	if h < 0 {
		h += 360
	}
	for _, c := range []struct {
		upTo float64
		name string
	}{{15, "red"}, {40, "orange"}, {65, "yellow"}, {90, "lime"}, {150, "green"}, {185, "teal"},
		{215, "sky"}, {250, "blue"}, {290, "purple"}, {340, "pink"}, {360, "red"}} {
		if h < c.upTo {
			return c.name
		}
	}
	return "red"
}
