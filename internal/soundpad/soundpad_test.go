package soundpad

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a list like Soundpad 4.0.35 saves, with what a simple one
// lacks: a stream, a relative path, a sound with a hotkey, an M4A, a
// missing file, sounds in a subfolder and twice in one list, a category
// naming a sound twice, a subcategory, names a folder can't have or that
// clash, empty categories and a sound in none.
const fixture = `<?xml version="1.0" encoding="UTF-8"?>
<Soundlist url="C:\Lists\mine.spl" saved="1">
  <Sound hash="1" url="C:\Program Files\Soundpad\sounds\ba dum tss.mp3" title="ba dum tss" duration="0:02" addedOn="2026-10-27" color="C9571C"/>
  <Sound hash="2" url="C:\Program Files\Soundpad\sounds\firework.mp3" title="firework" color="A20002" keyModifiers="0" key="13"/>
  <Sound hash="3" url="C:\S\Memes\bruh.mp3" title="bruh" color="2D9C89"/>
  <Sound hash="4" url="C:\S\Memes\Old\oof.wav" title="oof"/>
  <Sound hash="5" url="C:\S\Memes\clip.m4a" title="clip"/>
  <Sound hash="6" url="C:\S\Gone\gone.mp3" title="gone"/>
  <Sound hash="7" url="https://example.com/stream.mp3" title="stream"/>
  <Sound hash="8" url="Memes\rel.mp3" title="rel"/>
  <Sound hash="9" url="c:\s\memes\BRUH.mp3" title="bruh again"/>
  <Categories>
    <Category type="1" icon="stock_icon_cat_list" hidden="true"/>
    <Category name="Meine Sounds" icon="stock_icon_cat_home"><Sound id="0"/><Sound id="8"/><Sound id="0"/></Category>
    <Category name="Memes: best/worst" icon="gen_3"><Sound id="2"/>
      <Category name="Old"><Sound id="3"/><Sound id="5"/></Category>
    </Category>
    <Category name="meine sounds."><Sound id="2"/></Category>
    <Category name="Empty"/>
    <Category name="Streams"><Sound id="6"/><Sound id="x"/><Sound id="99"/></Category>
  </Categories>
  <Hotbar>
    <HotbarPage><HotbarItem id="0" soundId="2"/><HotbarItem id="1" soundId="0"/><HotbarItem id="2" soundId="x"/></HotbarPage>
    <HotbarPage/>
  </Hotbar>
</Soundlist>`

var fakeDisk = Disk{
	Size: func(p string) (int64, bool) { return 100, !strings.Contains(p, `\Gone\`) },
	CountSounds: func(dir string, limit int) (int, bool) {
		switch strings.ToLower(dir) {
		case `c:\program files\soundpad\sounds`:
			return 3, false // white_noise.mp3 too
		case `c:\s\memes`:
			return 9, false
		}
		return 0, false
	},
}

func TestParse(t *testing.T) {
	l, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Sounds) != 7 || l.Skipped != 2 || len(l.Categories) != 6 || l.SavedAs != `C:\Lists\mine.spl` {
		t.Fatalf("sounds %d skipped %d categories %d saved as %q", len(l.Sounds), l.Skipped, len(l.Categories), l.SavedAs)
	}
	s := l.Sounds
	if !s[0].Hotbar || s[0].Color != "orange" || s[0].Hotkey || !s[1].Hotkey || s[1].Color != "red" || !s[2].Hotbar || s[2].Color != "teal" || s[3].Hotbar {
		t.Errorf("sounds: %+v", s[:4])
	}
	// Positions are the parsed sounds': without the stream and the relative
	// path, BRUH.mp3 is 6.
	var cats []string
	for _, c := range l.Categories {
		cats = append(cats, fmt.Sprintf("%s%v", c.Title(), c.Sounds))
	}
	if got := strings.Join(cats, " "); got != "Meine Sounds[0 6 0] Memes: best/worst[2] Memes: best/worst › Old[3 5] meine sounds.[2] Empty[] Streams[]" {
		t.Errorf("categories: %s", got)
	}
	for _, bad := range []string{``, `<foo/>`, `{"data":1}`, `<Soundlist><Sound url=`} {
		if _, err := Parse([]byte(bad)); !errors.Is(err, ErrNotSoundpad) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestPlanList(t *testing.T) {
	l, _ := Parse([]byte(fixture))
	p := PlanList(l, fakeDisk)
	if len(p.Sounds) != 4 || p.Missing != 1 || p.Unplayable != 1 || p.HotkeySounds != 1 || p.Favorites() != 2 || p.Colored() != 3 {
		t.Fatalf("plan: sounds %d missing %d unplayable %d hotkeys %d favorites %d colored %d",
			len(p.Sounds), p.Missing, p.Unplayable, p.HotkeySounds, p.Favorites(), p.Colored())
	}
	if len(p.Folders) != 2 {
		t.Fatalf("folders: %+v", p.Folders)
	}
	sp, memes := p.Folders[0], p.Folders[1]
	if sp.Listed != 2 || sp.Files != 3 {
		t.Errorf("Soundpad's folder: %+v", sp)
	}
	// Old\oof.wav comes with Memes.
	if memes.Path != `C:\S\Memes` || memes.Listed != 2 || memes.Files != 9 {
		t.Errorf("Memes: %+v", memes)
	}

	// By category: each sound once per folder, the subcategory inside its
	// category, a clashing name told apart, empty ones left out, and
	// firework (in no category) in Other sounds.
	var cats []string
	for _, c := range p.Categories {
		cats = append(cats, fmt.Sprintf("%s=%s%v", c.Title, c.Dir, c.Sounds))
	}
	want := `Meine Sounds=Meine Sounds[0 2] Memes: best/worst=Memes_ best_worst[2] Memes: best/worst › Old=Memes_ best_worst\Old[3] ` +
		`meine sounds.=meine sounds (2)[2] Other sounds=Other sounds[1]`
	if got := strings.Join(cats, " "); got != want {
		t.Errorf("categories:\n got %s\nwant %s", got, want)
	}
	if files, bytes := p.Copies(); files != 6 || bytes != 600 {
		t.Errorf("copies: %d files, %d bytes", files, bytes)
	}
}

func TestFolderName(t *testing.T) {
	for in, want := range map[string]string{
		"Meine Sounds": "Meine Sounds", "3": "3", `a/b\c:d*e?f"g<h>i|j`: "a_b_c_d_e_f_g_h_i_j",
		"  .hidden  ": "hidden", "$Recycle": "Recycle", "dots...": "dots", "": "Category", "...": "Category",
		"CON": "CON_", "con.txt": "con.txt_", "COM1": "COM1_", "LPT9": "LPT9_", "COM10": "COM10", "Console": "Console",
		"tab\there": "tab_here", strings.Repeat("x", 90): strings.Repeat("x", 80),
	} {
		if got := FolderName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// Copying by category: names that clash in a folder get " (2)", a sound
// is copied once per category, an original that is gone is skipped, a
// copy run again finds its files, and a stopped one leaves no file.
func TestCopyByCategory(t *testing.T) {
	src, target := t.TempDir(), t.TempDir()
	write := func(rel, data string) string {
		p := filepath.Join(src, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(data), 0o644)
		return p
	}
	a, b1, b2, gone := write(`a.mp3`, "aaa"), write(`x\b.mp3`, "b1"), write(`y\b.mp3`, "b2-longer"), write(`gone.mp3`, "g")
	p := &Plan{
		Sounds: []Sound{{Path: a}, {Path: b1}, {Path: b2}, {Path: gone}},
		Sizes:  []int64{3, 2, 9, 1},
		Categories: []PlanCategory{
			{Title: "One", Dir: "One", Sounds: []int{1, 2, 0}},
			{Title: "One › Sub", Dir: `One\Sub`, Sounds: []int{0, 3}},
		},
	}
	os.Remove(gone)
	var last int
	c, err := CopyByCategory(target, p, func(n int) { last = n }, func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if c.Copied != 4 || c.Failed != 1 || c.Existing != 0 || last != 5 {
		t.Errorf("copied %d failed %d existing %d progress %d", c.Copied, c.Failed, c.Existing, last)
	}
	rel := func(p string) string {
		if p == "" {
			return ""
		}
		r, _ := filepath.Rel(target, p)
		return r
	}
	got := fmt.Sprint(rel(c.Paths[0][0]), "|", rel(c.Paths[0][1]), "|", rel(c.Paths[0][2]), "|", rel(c.Paths[1][0]), "|", rel(c.Paths[1][1]))
	if want := `One\b.mp3|One\b (2).mp3|One\a.mp3|One\Sub\a.mp3|`; got != want {
		t.Errorf("paths %s, want %s", got, want)
	}
	if data, _ := os.ReadFile(filepath.Join(target, `One\b (2).mp3`)); string(data) != "b2-longer" {
		t.Errorf("b (2).mp3: %q", data)
	}

	c, err = CopyByCategory(target, p, func(int) {}, func() bool { return false })
	if err != nil || c.Copied != 0 || c.Existing != 4 || rel(c.Paths[0][1]) != `One\b (2).mp3` {
		t.Errorf("again: %+v, %v", c, err)
	}

	other := t.TempDir()
	if _, err := CopyByCategory(other, p, func(int) {}, func() bool { return true }); !errors.Is(err, ErrStopped) {
		t.Errorf("stopped: %v", err)
	}
	filepath.WalkDir(other, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			t.Errorf("left after stopping: %s", p)
		}
		return nil
	})
}

func TestPhoneColor(t *testing.T) {
	for hex, want := range map[string]string{
		"C9571C": "orange", "A20002": "red", "2D9C89": "teal", "FFFFFF": "grey", "000000": "grey", "808080": "grey",
		"FF00FF": "pink", "0000FF": "blue", "00FF00": "green", "FFFF00": "yellow", "3399FF": "sky", "8000FF": "purple",
		"": "", "12345": "", "GGGGGG": "",
	} {
		if got := PhoneColor(hex); got != want {
			t.Errorf("%q: %q, want %q", hex, got, want)
		}
	}
}

func TestParseSettings(t *testing.T) {
	values := func(m map[string]uint32) func(string) (uint32, bool) {
		return func(n string) (uint32, bool) { v, ok := m[n]; return v, ok }
	}
	// The developer's: stop Alt+H, pause Ctrl+Q, an auto key without a key
	// (with a stale modifier) and one on 4, the mic muted while playing.
	s := parseSettings(values(map[string]uint32{"stopKey": 0x48, "stopKeyModifiers": 1, "pauseResumeKey": 0x51, "pauseResumeKeyModifiers": 2,
		"distinguishLeftRightModifiers": 0, "filterBlockVoice": 1}),
		values(map[string]uint32{
			"keyModifiers0": 1, "key0": 0xFFFFFFFF, "enabled0": 1, "onlyByHotkey0": 1, "type0": 0, "condition0": 0,
			"keyModifiers1": 0, "key1": 0x34, "enabled1": 1, "onlyByHotkey1": 1, "type1": 0, "condition1": 0,
		}))
	if !s.Found || fmt.Sprint(s.Stop) != "[164 72]" || fmt.Sprint(s.Pause) != "[162 81]" || fmt.Sprint(s.PushToTalk) != "[[52]]" || len(s.Skipped) != 0 || !s.MuteMic {
		t.Errorf("developer's settings: %+v", s)
	}
	s = parseSettings(values(map[string]uint32{"stopKey": 0x48, "stopKeyModifiers": 0xF, "pauseResumeKey": 0x02, "filterBlockVoice": 2}),
		values(map[string]uint32{
			"key0": 0x56, "enabled0": 1, // V: push-to-talk
			"key1": 0x05, "enabled1": 1, // Mouse 4
			"key2": 0x42, "enabled2": 0, // off
			"key3": 0x43, "enabled3": 1, "type3": 1, // before and after
			"key4": 0x44, "enabled4": 1, "keyModifiers4": 0x10, // an unknown bit
			"key5": 0x45, "enabled5": 1, "keyModifiers5": 4, // Shift+E
		}))
	if fmt.Sprint(s.Stop) != "[162 160 164 91 72]" || s.Pause != nil || fmt.Sprint(s.PushToTalk) != "[[86] [5] [160 69]]" || len(s.Skipped) != 3 || s.MuteMic {
		t.Errorf("settings: %+v", s)
	}
	// Left and right modifiers told apart: the field can't be read.
	s = parseSettings(values(map[string]uint32{"stopKey": 0x48, "stopKeyModifiers": 2, "pauseResumeKey": 0x51, "distinguishLeftRightModifiers": 1}),
		values(nil))
	if s.Stop != nil || fmt.Sprint(s.Pause) != "[81]" || len(s.Skipped) != 1 {
		t.Errorf("left and right: %+v", s)
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	for _, f := range []string{CurrentList, "mine.spl", "stats.spl", "^mine.spl", "notes.txt"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(other, "elsewhere.spl"), []byte("x"), 0o644)
	got := Discover(dir, []string{filepath.Join(other, "elsewhere.spl"), filepath.Join(dir, "MINE.spl"), filepath.Join(other, "deleted.spl"), "relative.spl"})
	var names []string
	for _, p := range got {
		names = append(names, filepath.Base(p))
	}
	if strings.Join(names, ",") != CurrentList+",elsewhere.spl,MINE.spl" {
		t.Errorf("lists: %v", names)
	}
}

// TestLiveSoundpad reads this PC's Soundpad settings (registry, read only)
// and lists, and plans each. RESOUND_LIVE=1.
func TestLiveSoundpad(t *testing.T) {
	if os.Getenv("RESOUND_LIVE") != "1" {
		t.Skip("set RESOUND_LIVE=1 to read Soundpad's settings and lists")
	}
	s, recent := ReadSettings()
	t.Logf("settings found %v: push-to-talk %v, stop %v, pause %v, mute mic %v, skipped %q; recent %q", s.Found, s.PushToTalk, s.Stop, s.Pause, s.MuteMic, s.Skipped, recent)
	for _, path := range Discover(Dir(), recent) {
		l, err := Load(path)
		if err != nil {
			t.Logf("%s: %v", path, err)
			continue
		}
		t.Logf("%s: current %v, saved as %q, %d sounds, %d categories, skipped %d", filepath.Base(path), l.Current, l.SavedAs, len(l.Sounds), len(l.Categories), l.Skipped)
		p := PlanList(l, OS)
		files, bytes := p.Copies()
		t.Logf("  sounds %d, missing %d, unplayable %d, hotkeys %d, favorites %d, colored %d; by category %d files, %d bytes",
			len(p.Sounds), p.Missing, p.Unplayable, p.HotkeySounds, p.Favorites(), p.Colored(), files, bytes)
		for _, f := range p.Folders {
			t.Logf("  folder %s: %d listed of %d (many %v)", f.Path, f.Listed, f.Files, f.Many)
		}
		for _, c := range p.Categories {
			var names []string
			for _, i := range c.Sounds {
				names = append(names, filepath.Base(p.Sounds[i].Path))
			}
			t.Logf("  category %q → %s: %s", c.Title, c.Dir, strings.Join(names, ", "))
		}
	}
}
