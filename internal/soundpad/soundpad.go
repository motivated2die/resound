// Package soundpad reads Soundpad's sound lists and keys, so the guided
// setup can carry them over. Soundpad keeps sounds from anywhere in lists of
// files (.spl, XML) and groups them into categories freely; Resound's tabs
// are folders. So PlanList works out both ways the setup offers: the folders
// a list's sounds are in (added as they are), and a folder per category
// (copy.go copies the sounds into them).
//
// Nothing is trusted: a list is parsed with limits, every path is checked
// on the disk, and keys are taken only where their encoding is known
// (settings.go). Soundpad's files and registry key are only read.
//
// The format is Soundpad 4's (checked with 4.0.35): <Soundlist> holds
// <Sound url title color [key keyModifiers]> elements, then <Categories>
// and <Hotbar> that refer to the sounds by their 0-based position. A
// category lists its sounds in Soundpad's order and may name one twice;
// subcategories are read as <Category> elements inside it (not seen in a
// real list yet). The same file may be in a list more than once.
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
	// sounds" one), each followed by its subcategories.
	Categories []Category
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

// Category is one of the user's categories.
type Category struct {
	// Path is its name and those of the categories it is in, top first.
	Path []string
	// Sounds are positions in List.Sounds, in Soundpad's order.
	Sounds []int
}

// Title is the category's name as a tab shows a nested folder's:
// "Memes › Old".
func (c Category) Title() string { return strings.Join(c.Path, " › ") }

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
	Categories []xmlCategory `xml:"Categories>Category"`
	Hotbar     []struct {
		Items []struct {
			SoundID string `xml:"soundId,attr"`
		} `xml:"HotbarItem"`
	} `xml:"Hotbar>HotbarPage"`
}

type xmlCategory struct {
	Name   string `xml:"name,attr"`
	Type   string `xml:"type,attr"`
	Sounds []struct {
		ID string `xml:"id,attr"`
	} `xml:"Sound"`
	Sub []xmlCategory `xml:"Category"`
}

// maxCategoryDepth is how deep subcategories are read; deeper ones are left
// out (the library scans 8 folders deep).
const maxCategoryDepth = 6

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
	at := map[int]int{} // a position in the file → in l.Sounds
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
		at[i] = len(l.Sounds)
		l.Sounds = append(l.Sounds, Sound{
			Path:   filepath.Clean(s.URL),
			Title:  s.Title,
			Color:  PhoneColor(s.Color),
			Hotbar: hotbar[i],
			Hotkey: err == nil && key > 0,
		})
	}
	var walk func(cats []xmlCategory, path []string)
	walk = func(cats []xmlCategory, path []string) {
		for _, c := range cats {
			if c.Type != "" || c.Name == "" || len(path) == maxCategoryDepth {
				continue // Soundpad's own "all sounds", or damaged
			}
			cat := Category{Path: append(slices.Clone(path), c.Name)}
			for _, snd := range c.Sounds {
				if id, err := strconv.Atoi(snd.ID); err == nil {
					if j, ok := at[id]; ok {
						cat.Sounds = append(cat.Sounds, j)
					}
				}
			}
			l.Categories = append(l.Categories, cat)
			walk(c.Sub, cat.Path)
		}
	}
	walk(x.Categories, nil)
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

// Plan is what an import of a list would carry over, checked against the
// disk, for both ways: by folder (Folders) and by category (Categories).
type Plan struct {
	// Folders hold the list's sounds, each with the sounds in its
	// subfolders, in the order the list first names them.
	Folders []Folder
	// Sounds are the sounds whose files exist and play, once each.
	Sounds []Sound
	// Sizes are the Sounds' file sizes.
	Sizes []int64
	// Categories are the folders the sounds are copied into by category:
	// the list's categories, then OtherSounds for sounds in none.
	Categories []PlanCategory
	// Missing counts sounds whose files are gone; Unplayable those in a
	// format Resound doesn't play (Soundpad plays M4A, WMA, AAC and more).
	Missing, Unplayable int
	// HotkeySounds counts sounds with a hotkey of their own; it doesn't
	// carry over.
	HotkeySounds int
	Skipped      int
}

// Folder is a folder with sounds of the list.
type Folder struct {
	Path string
	// Listed counts the list's sounds in it (and its subfolders); Files
	// all the sounds Resound would find there, Many when there are more
	// than it counted.
	Listed, Files int
	Many          bool
}

// PlanCategory is a category as a folder to copy sounds into.
type PlanCategory struct {
	Title string // Category.Title, or OtherSounds
	// Dir is the folder below the chosen one: the names made safe for a
	// folder (FolderName), "Memes\Old" for a subcategory.
	Dir string
	// Sounds are positions in Plan.Sounds, in Soundpad's order, each once.
	Sounds []int
	// Other is the folder of sounds in no category.
	Other bool
}

// OtherSounds names the folder of sounds that are in no category.
const OtherSounds = "Other sounds"

// Disk is what Plan asks about files; tests replace it.
type Disk struct {
	// Size is a file's size, false when it isn't a file.
	Size func(path string) (int64, bool)
	// CountSounds counts the playable files in dir and its subfolders, up
	// to limit, and reports whether it stopped there.
	CountSounds func(dir string, limit int) (n int, more bool)
}

// OS is the real disk.
var OS = Disk{
	Size: func(p string) (int64, bool) {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			return 0, false
		}
		return info.Size(), true
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

// PlanList checks a list against the disk.
func PlanList(l *List, disk Disk) Plan {
	p := Plan{Skipped: l.Skipped}
	at := map[int]int{}        // a position in l.Sounds → in p.Sounds
	byPath := map[string]int{} // a file → its position in p.Sounds
	gone := map[string]bool{}  // files that are missing or don't play
	byDir := map[string]int{}
	for i, s := range l.Sounds {
		k := strings.ToLower(s.Path)
		if j, ok := byPath[k]; ok {
			at[i] = j // listed again: the same sound
			p.Sounds[j].Hotbar = p.Sounds[j].Hotbar || s.Hotbar
			if p.Sounds[j].Color == "" {
				p.Sounds[j].Color = s.Color
			}
			continue
		}
		if gone[k] {
			continue
		}
		if s.Hotkey {
			p.HotkeySounds++
		}
		size, ok := int64(0), Playable(s.Path)
		if !ok {
			p.Unplayable++
		} else if size, ok = disk.Size(s.Path); !ok {
			p.Missing++
		}
		if !ok {
			gone[k] = true
			continue
		}
		at[i], byPath[k] = len(p.Sounds), len(p.Sounds)
		p.Sounds = append(p.Sounds, s)
		p.Sizes = append(p.Sizes, size)
		dir := filepath.Dir(s.Path)
		j, ok := byDir[strings.ToLower(dir)]
		if !ok {
			j = len(p.Folders)
			byDir[strings.ToLower(dir)] = j
			p.Folders = append(p.Folders, Folder{Path: dir})
		}
		p.Folders[j].Listed++
	}
	p.Folders = topFolders(p.Folders, disk)
	p.Categories = planCategories(l.Categories, at, len(p.Sounds))
	return p
}

// topFolders merges each folder into the one it is inside, if any, and
// counts the sounds in each.
func topFolders(folders []Folder, disk Disk) []Folder {
	var top []Folder
	for i, f := range folders {
		in := -1
		for j, g := range folders {
			if i != j && within(f.Path, g.Path) && (in < 0 || len(g.Path) < len(folders[in].Path)) {
				in = j
			}
		}
		if in < 0 {
			top = append(top, f)
		}
	}
	for i := range top {
		top[i].Listed = 0
		for _, f := range folders {
			if strings.EqualFold(f.Path, top[i].Path) || within(f.Path, top[i].Path) {
				top[i].Listed += f.Listed
			}
		}
		const countLimit = 5000
		top[i].Files, top[i].Many = disk.CountSounds(top[i].Path, countLimit)
		top[i].Files = max(top[i].Files, top[i].Listed)
	}
	return top
}

// planCategories turns the list's categories into folders: each sound once
// per category, names made safe and told apart, and sounds in no category
// in OtherSounds. at maps the list's sounds to the plan's (missing ones
// aren't there); n is the plan's number of sounds. Empty categories make
// no folder.
func planCategories(cats []Category, at map[int]int, n int) []PlanCategory {
	var out []PlanCategory
	used := map[string]map[string]bool{} // a parent folder → its folders' names, lower case
	dirOf := map[string]string{}         // a category's path → its folder
	unique := func(parent, name string) string {
		if used[parent] == nil {
			used[parent] = map[string]bool{}
		}
		try := name
		for i := 2; used[parent][strings.ToLower(try)]; i++ {
			try = fmt.Sprintf("%s (%d)", name, i)
		}
		used[parent][strings.ToLower(try)] = true
		return try
	}
	in := make([]bool, n)
	for _, c := range cats {
		parent := ""
		if len(c.Path) > 1 {
			parent = dirOf[strings.Join(c.Path[:len(c.Path)-1], "\x00")]
		}
		dir := unique(parent, FolderName(c.Path[len(c.Path)-1]))
		if parent != "" {
			dir = filepath.Join(parent, dir)
		}
		dirOf[strings.Join(c.Path, "\x00")] = dir
		pc := PlanCategory{Title: c.Title(), Dir: dir}
		seen := map[int]bool{}
		for _, i := range c.Sounds {
			if j, ok := at[i]; ok && !seen[j] {
				seen[j], in[j] = true, true
				pc.Sounds = append(pc.Sounds, j)
			}
		}
		out = append(out, pc)
	}
	other := PlanCategory{Title: OtherSounds, Other: true}
	for j, ok := range in {
		if !ok {
			other.Sounds = append(other.Sounds, j)
		}
	}
	if len(other.Sounds) > 0 {
		other.Dir = unique("", OtherSounds)
		out = append(out, other)
	}
	return slices.DeleteFunc(out, func(c PlanCategory) bool { return len(c.Sounds) == 0 })
}

// FolderName makes a category's name a folder's: characters Windows doesn't
// allow become "_", leading dots and "$" go (the library skips such
// folders), as do trailing dots and spaces, and a reserved device name
// ("CON", "COM1") gets a "_". At most 80 characters; "Category" for none.
func FolderName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			r = '_'
		}
		b.WriteRune(r)
	}
	trim := func(s string) string { return strings.TrimRight(strings.TrimLeft(s, " .$"), " .") }
	s := trim(b.String())
	if r := []rune(s); len(r) > 80 {
		s = trim(string(r[:80]))
	}
	if s == "" {
		return "Category"
	}
	stem := strings.ToUpper(strings.TrimSpace(strings.SplitN(s, ".", 2)[0]))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return s + "_"
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9' {
		return s + "_"
	}
	return s
}

// Copies counts the files a copy by category makes (a sound once per
// category) and their size.
func (p *Plan) Copies() (files int, bytes int64) {
	for _, c := range p.Categories {
		for _, i := range c.Sounds {
			files++
			bytes += p.Sizes[i]
		}
	}
	return files, bytes
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
