package soundux

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/library"
)

// fixture has what the developer's real config lacks: hotkeys, favorites,
// nested and missing folders, damaged entries, the newer key format.
const fixture = `{"data":{"height":720,"width":1280,"soundIdCounter":9,"tabs":[
 {"id":0,"name":"Memes","path":"C:/S/Memes","sortMode":2,"sounds":[
  {"id":1,"name":"bruh","path":"C:/S/Memes/bruh.mp3","isFavorite":true,"hotkeys":[162,66],"hotkeySequence":"Ctrl + B","localVolume":50,"remoteVolume":null,"modifiedDate":1},
  {"id":2,"name":"gone","path":"C:/S/Memes/gone.mp3","isFavorite":true,"hotkeys":[],"localVolume":null,"remoteVolume":null,"modifiedDate":1},
  {"id":3,"name":"video","path":"C:/S/Memes/clip.m4a","isFavorite":true,"hotkeys":[],"modifiedDate":1},
  {"id":4,"path":42},
  {"id":5,"name":"rel","path":"Memes/rel.mp3","hotkeys":[],"modifiedDate":1},
  {"id":6,"name":"loud","path":"C:/S/Memes/loud.mp3","hotkeys":[{"key":5,"type":1}],"remoteVolume":150,"localVolume":"x","modifiedDate":1}]},
 {"id":1,"name":"Sub","path":"C:/S/Memes/Sub","sortMode":0,"sounds":[]},
 {"id":2,"name":"USB","path":"E:/Sounds","sortMode":1,"sounds":[{"id":7,"path":"E:/Sounds/a.mp3","isFavorite":true,"hotkeys":[]}]},
 {"id":3,"name":"dupe","path":"c:\\s\\memes","sounds":[]},
 "garbage"]},
 "settings":{"pushToTalkKeys":[86],"stopHotkey":[86],"outputs":["Voicemeeter Input (VB-Audio Voicemeeter VAIO)","CABLE Input (VB-Audio Virtual Cable)"],
  "localVolume":"50","remoteVolume":100,"allowOverlapping":false,"muteDuringPlayback":true,"allowMultipleOutputs":true,"theme":1}}`

var fakeDisk = Disk{
	IsDir: func(p string) bool {
		return strings.EqualFold(p, `C:\S\Memes`) || strings.EqualFold(p, `C:\S\Memes\Sub`)
	},
	IsFile: func(p string) bool {
		return strings.EqualFold(p, `C:\S\Memes\bruh.mp3`) || strings.EqualFold(p, `C:\S\Memes\loud.mp3`) || strings.EqualFold(p, `C:\S\Memes\clip.m4a`)
	},
}

func TestParseFixture(t *testing.T) {
	f, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Tabs) != 4 || f.Skipped != 3 { // "garbage", path 42, relative path
		t.Fatalf("tabs %d skipped %d", len(f.Tabs), f.Skipped)
	}
	m := f.Tabs[0]
	if m.Path != `C:\S\Memes` || m.SortMode != SortNameAsc || len(m.Sounds) != 4 {
		t.Fatalf("tab 0: %+v", m)
	}
	if b := m.Sounds[0]; !b.Favorite || len(b.Hotkeys) != 2 || *b.LocalVolume != 50 || b.RemoteVolume != nil {
		t.Errorf("bruh: %+v", b)
	}
	if l := m.Sounds[3]; l.LocalVolume != nil || *l.RemoteVolume != 150 || l.Hotkeys != nil {
		t.Errorf("loud: a wrong type or a mouse key of the newer format must drop only that field: %+v", l)
	}
	s := f.Settings
	if s.LocalVolume != nil || *s.RemoteVolume != 100 || *s.AllowOverlapping || !*s.MuteDuringPlayback {
		t.Errorf("settings: %+v", s)
	}
}

func TestPlanFixture(t *testing.T) {
	f, _ := Parse([]byte(fixture))
	p := f.Plan(fakeDisk)
	if len(p.Folders) != 2 || !p.Folders[1].Covered || p.Folders[0].Covered || p.Folders[0].Sort != "name-asc" || !p.Folders[1].OldestFirst {
		t.Errorf("folders: %+v", p.Folders)
	}
	if len(p.MissingFolders) != 1 || p.MissingFolders[0] != `E:\Sounds` {
		t.Errorf("missing folders: %v", p.MissingFolders)
	}
	// gone.mp3 is missing; clip.m4a exists but Resound can't play it.
	if len(p.Sounds) != 2 || p.MissingSounds != 2 {
		t.Fatalf("sounds %+v, missing %d", p.Sounds, p.MissingSounds)
	}
	if v := p.Sounds[0].LocalVolume; *v != audio.FromLinear(50) {
		t.Errorf("50%% linear became %d", *v)
	}
	if v := p.Sounds[1].RemoteVolume; *v != 150 {
		t.Errorf("a boost stays linear: %d", *v)
	}
	if p.HotkeySounds != 1 || p.Favorites() != 1 || p.Volumes() != 2 || p.SoundCount() != 2 {
		t.Errorf("hotkey sounds %d, favorites %d, volumes %d, sounds %d", p.HotkeySounds, p.Favorites(), p.Volumes(), p.SoundCount())
	}
	if p.Stop != nil || len(p.PushToTalk) != 1 || len(p.DroppedKeys) != 1 {
		t.Errorf("keys: ptt %v stop %v dropped %v", p.PushToTalk, p.Stop, p.DroppedKeys)
	}
	if p.MonitorVolume != nil || *p.CableVolume != 100 || !*p.NoSoundOverlap || !*p.MuteMic {
		t.Errorf("options: %+v", p)
	}
	if p.Empty() {
		t.Error("empty")
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{``, `[]`, `{"foo":1}`, `{"data":`, `null`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%q: parsed", in)
		}
	}
	if _, err := Parse([]byte(`{"foo":1}`)); !errors.Is(err, ErrNotSoundux) {
		t.Errorf("want ErrNotSoundux, got %v", err)
	}
	// An empty Soundux (first start, nothing added) parses to nothing.
	f, err := Parse([]byte(`{"data":{"tabs":[]},"settings":{}}`))
	if err != nil || len(f.Tabs) != 0 {
		t.Fatalf("empty: %v %v", f, err)
	}
	if p := f.Plan(fakeDisk); !p.Empty() {
		t.Errorf("an empty Soundux has something to carry over: %+v", p)
	}
}

// TestLiveSoundux plans the import of this PC's real Soundux config and
// applies its library part to a copy of the real Resound index: sounds are
// found by path, tabs by folder. RESOUND_LIVE=1; nothing real is changed.
func TestLiveSoundux(t *testing.T) {
	if os.Getenv("RESOUND_LIVE") != "1" {
		t.Skip("set RESOUND_LIVE=1 to read %APPDATA%\\Soundux\\config.json")
	}
	f, err := Load(Path())
	if err != nil {
		t.Skip(err)
	}
	p := f.Plan(OS)
	t.Logf("folders %d, missing folders %v, sounds with settings %d, missing %d, hotkey sounds %d, skipped %d",
		len(p.Folders), p.MissingFolders, len(p.Sounds), p.MissingSounds, p.HotkeySounds, p.Skipped)
	for _, fo := range p.Folders {
		t.Logf("  %s (%q) sort %q oldestFirst %v sounds %d covered %v", fo.Path, fo.Name, fo.Sort, fo.OldestFirst, fo.Sounds, fo.Covered)
	}
	for _, s := range p.Sounds {
		t.Logf("  %s fav %v local %v remote %v", filepath.Base(s.Path), s.Favorite, deref(s.LocalVolume), deref(s.RemoteVolume))
	}
	t.Logf("ptt %v stop %v dropped %v monitor %v cable %v noOverlap %v muteMic %v",
		p.PushToTalk, p.Stop, p.DroppedKeys, deref(p.MonitorVolume), deref(p.CableVolume), derefB(p.NoSoundOverlap), derefB(p.MuteMic))

	// The library side, on a copy of the real index.
	dir := t.TempDir()
	if data, err := os.ReadFile(filepath.Join(os.Getenv("LOCALAPPDATA"), "Resound", "library.json")); err == nil {
		os.WriteFile(filepath.Join(dir, "library.json"), data, 0o644)
	}
	lib := library.Open(dir, audio.SupportedExtensions)
	defer lib.Close()
	var folders []string
	for _, fo := range p.Folders {
		if !fo.Covered {
			folders = append(folders, fo.Path)
		}
	}
	lib.Scan(folders)
	found := 0
	for _, s := range p.Sounds {
		if _, ok := lib.FindPath(s.Path); ok {
			found++
		} else {
			t.Errorf("not in the library after the scan: %s", s.Path)
		}
	}
	tabs := 0
	for _, fo := range p.Folders {
		for _, tab := range lib.Tabs() {
			if strings.EqualFold(tab.Path, fo.Path) {
				tabs++
				t.Logf("  tab %d %q: %d sounds in Resound, %d listed by Soundux", tab.ID, tab.Name, len(tab.Sounds), fo.Sounds)
			}
		}
	}
	if tabs != len(p.Folders) {
		t.Errorf("%d of %d folders became tabs", tabs, len(p.Folders))
	}
	t.Logf("%d of %d sounds with settings found", found, len(p.Sounds))
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefB(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}
