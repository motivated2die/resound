package soundpad

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a list like Soundpad 4.0.35 saves, with what a simple one
// lacks: a stream, a relative path, a sound with a hotkey, an M4A, a
// missing file, sounds in a subfolder and twice in one list.
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
    <Category name="Meine Sounds" icon="stock_icon_cat_home"><Sound id="0"/><Sound id="1"/></Category>
    <Category name="3" icon="gen_3"><Sound id="2"/></Category>
  </Categories>
  <Hotbar>
    <HotbarPage><HotbarItem id="0" soundId="2"/><HotbarItem id="1" soundId="0"/><HotbarItem id="2" soundId="x"/></HotbarPage>
    <HotbarPage/>
  </Hotbar>
</Soundlist>`

var fakeDisk = Disk{
	IsFile: func(p string) bool { return !strings.Contains(p, `\Gone\`) },
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
	if len(l.Sounds) != 7 || l.Skipped != 2 || len(l.Categories) != 2 || l.SavedAs != `C:\Lists\mine.spl` {
		t.Fatalf("sounds %d skipped %d categories %d saved as %q", len(l.Sounds), l.Skipped, len(l.Categories), l.SavedAs)
	}
	s := l.Sounds
	if !s[0].Hotbar || s[0].Color != "orange" || s[0].Hotkey || !s[1].Hotkey || s[1].Color != "red" || !s[2].Hotbar || s[2].Color != "teal" || s[3].Hotbar {
		t.Errorf("sounds: %+v", s[:4])
	}
	for _, bad := range []string{``, `<foo/>`, `{"data":1}`, `<Soundlist><Sound url=`} {
		if _, err := Parse([]byte(bad)); !errors.Is(err, ErrNotSoundpad) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestPlanLists(t *testing.T) {
	l, _ := Parse([]byte(fixture))
	p := PlanLists([]*List{l, l}, fakeDisk) // a list chosen twice adds nothing
	if len(p.Sounds) != 4 || p.Categories != 2 || p.Missing != 1 || p.Unplayable != 1 || p.HotkeySounds != 1 || p.Favorites() != 2 || p.Colored() != 3 {
		t.Fatalf("plan: sounds %d missing %d unplayable %d hotkeys %d favorites %d colored %d",
			len(p.Sounds), p.Missing, p.Unplayable, p.HotkeySounds, p.Favorites(), p.Colored())
	}
	if len(p.Folders) != 2 {
		t.Fatalf("folders: %+v", p.Folders)
	}
	sp, memes := p.Folders[0], p.Folders[1]
	if sp.Listed != 2 || sp.Files != 3 || !sp.Suggested() {
		t.Errorf("Soundpad's folder: %+v", sp)
	}
	// Old\oof.wav comes with Memes; most of Memes' sounds aren't listed.
	if memes.Path != `C:\S\Memes` || memes.Listed != 2 || memes.Files != 9 || memes.Suggested() {
		t.Errorf("Memes: %+v", memes)
	}
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
	// The developer's: stop H, pause \, an auto key without a key.
	s := parseSettings(values(map[string]uint32{"stopKey": 0x48, "stopKeyModifiers": 0, "pauseResumeKey": 0xDC, "pauseResumeKeyModifiers": 0}),
		values(map[string]uint32{"keyModifiers0": 1, "key0": 0xFFFFFFFF, "enabled0": 1, "onlyByHotkey0": 1, "type0": 0, "condition0": 0}))
	if !s.Found || len(s.Stop) != 1 || s.Stop[0] != 0x48 || len(s.Pause) != 1 || s.Pause[0] != 0xDC || s.PushToTalk != nil || len(s.Skipped) != 0 {
		t.Errorf("developer's settings: %+v", s)
	}
	s = parseSettings(values(map[string]uint32{"stopKey": 0x48, "stopKeyModifiers": 2, "pauseResumeKey": 0x02}),
		values(map[string]uint32{
			"key0": 0x56, "enabled0": 1, // V: push-to-talk
			"key1": 0x05, "enabled1": 1, // Mouse 4
			"key2": 0x42, "enabled2": 0, // off
			"key3": 0x43, "enabled3": 1, "type3": 1, // before and after
			"key4": 0x44, "enabled4": 1, "keyModifiers4": 1,
		}))
	if s.Stop != nil || s.Pause != nil || len(s.PushToTalk) != 2 || s.PushToTalk[1][0] != 0x05 || len(s.Skipped) != 4 {
		t.Errorf("settings: %+v", s)
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
// and lists, and plans them. RESOUND_LIVE=1.
func TestLiveSoundpad(t *testing.T) {
	if os.Getenv("RESOUND_LIVE") != "1" {
		t.Skip("set RESOUND_LIVE=1 to read Soundpad's settings and lists")
	}
	s, recent := ReadSettings()
	t.Logf("settings found %v: push-to-talk %v, stop %v, pause %v, skipped %q; recent %q", s.Found, s.PushToTalk, s.Stop, s.Pause, s.Skipped, recent)
	var lists []*List
	for _, path := range Discover(Dir(), recent) {
		l, err := Load(path)
		if err != nil {
			t.Logf("%s: %v", path, err)
			continue
		}
		t.Logf("%s: current %v, saved as %q, %d sounds, %d categories, skipped %d", filepath.Base(path), l.Current, l.SavedAs, len(l.Sounds), len(l.Categories), l.Skipped)
		lists = append(lists, l)
	}
	if len(lists) == 0 {
		t.Skip("no lists")
	}
	p := PlanLists(lists, OS)
	t.Logf("sounds %d, missing %d, unplayable %d, hotkeys %d, categories %d, favorites %d, colored %d",
		len(p.Sounds), p.Missing, p.Unplayable, p.HotkeySounds, p.Categories, p.Favorites(), p.Colored())
	for _, f := range p.Folders {
		t.Logf("  %s: %d listed of %d (many %v), suggested %v", f.Path, f.Listed, f.Files, f.Many, f.Suggested())
	}
}
