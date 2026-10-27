package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/soundux"
)

// sounduxApp has the keys of keybindsApp and a library of two folders: A
// (a.mp3, b.mp3, c.mp3) and B (d.mp3), where b.mp3 has a volume of its own
// and B is sorted on the phone already.
func sounduxApp(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{`A\a.mp3`, `A\b.mp3`, `A\c.mp3`, `B\d.mp3`} {
		p := filepath.Join(root, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	a := keybindsApp()
	a.lib = library.Open(t.TempDir(), audio.SupportedExtensions)
	t.Cleanup(func() { a.lib.Close() })
	a.cfg.Folders = []string{filepath.Join(root, "A"), filepath.Join(root, "B")}
	a.lib.Scan(a.cfg.Folders)
	ten := 10
	a.lib.Update(sxID(t, a, root, `A\b.mp3`), func(s *library.SoundSettings) { s.LocalVolume = &ten })
	for _, tab := range a.lib.Tabs() {
		if filepath.Base(tab.Path) == "B" {
			a.lib.UpdateLayout(strconv.FormatUint(tab.ID, 10), func(l *library.Layout) { l.Sort = library.SortNameDesc })
		}
	}
	return a, root
}

func sxID(t *testing.T, a *App, root, rel string) uint64 {
	t.Helper()
	id, ok := a.lib.FindPath(filepath.Join(root, rel))
	if !ok {
		t.Fatalf("%s not in the library", rel)
	}
	return id
}

func intp(v int) *int    { return &v }
func boolp(v bool) *bool { return &v }

func sounduxPlan(root string) *soundux.Plan {
	return &soundux.Plan{
		Folders: []soundux.Folder{
			{Path: filepath.Join(root, "A"), Sort: "name-asc", Sounds: 3},
			{Path: filepath.Join(root, "B"), Sort: "name-asc", Sounds: 1},
		},
		Sounds: []soundux.SoundSettings{
			{Path: filepath.Join(root, `A\a.mp3`), Favorite: true, RemoteVolume: intp(40)},
			{Path: filepath.Join(root, `A\b.mp3`), Favorite: true, LocalVolume: intp(90)},
			{Path: filepath.Join(root, `Elsewhere\e.mp3`), Favorite: true},
		},
		PushToTalk:     []uint16{vkV},
		Stop:           []uint16{vkG}, // a pause key in Resound
		MonitorVolume:  intp(45),
		CableVolume:    intp(63),
		NoSoundOverlap: boolp(true),
		MuteMic:        boolp(true),
	}
}

func TestApplySoundux(t *testing.T) {
	a, root := sounduxApp(t)
	a.cfg.MuteMicMinSeconds = 5
	p := sounduxPlan(root)
	a.soundux.plan = p
	r := a.applySoundux(p)

	snd := func(rel string) library.Sound {
		s, _, _ := a.lib.Sound(sxID(t, a, root, rel))
		return s
	}
	if s := snd(`A\a.mp3`); !s.Favorite || s.LocalVolume != nil || s.RemoteVolume == nil || *s.RemoteVolume != 40 {
		t.Errorf("a.mp3: %+v", s.SoundSettings)
	}
	if s := snd(`A\b.mp3`); !s.Favorite || *s.LocalVolume != 10 || s.RemoteVolume != nil {
		t.Errorf("b.mp3 must keep the volume set in Resound: %+v", s.SoundSettings)
	}
	if r.favorites != 2 || r.volumes != 1 {
		t.Errorf("favorites %d volumes %d; e.mp3 isn't in the library", r.favorites, r.volumes)
	}
	for _, tab := range a.lib.Tabs() {
		want := map[string]string{"A": library.SortNameAsc, "B": library.SortNameDesc}[filepath.Base(tab.Path)]
		if got := a.lib.Layout(strconv.FormatUint(tab.ID, 10)).Sort; got != want {
			t.Errorf("tab %s sorted %q, want %q (sorted on the phone already: kept)", tab.Name, got, want)
		}
	}
	if r.sorted != 1 {
		t.Errorf("sorted %d", r.sorted)
	}

	if ptt := a.globalKeys(fnPTT); len(ptt) != 2 || ptt[0][0] != vkPause || ptt[1][0] != vkV || !a.cfg.PushToTalkWhilePlaying {
		t.Errorf("push-to-talk: %v", ptt)
	}
	if stop := a.globalKeys(fnStop); len(stop) != 1 || stop[0][0] != vkX {
		t.Errorf("stop: %v", stop)
	}
	if r.keys != 1 || len(r.notes) != 1 || !strings.Contains(r.notes[0], "pause key") {
		t.Errorf("keys %d, notes %q", r.keys, r.notes)
	}

	if a.cfg.MonitorVolume != 45 || a.cfg.CableVolume != 63 || !a.cfg.NoSoundOverlap {
		t.Errorf("volumes %d %d, no overlap %v", a.cfg.MonitorVolume, a.cfg.CableVolume, a.cfg.NoSoundOverlap)
	}
	if !a.cfg.MuteMicWhilePlaying || a.cfg.MuteMicMinSeconds != 0 {
		t.Errorf("Soundux muted the mic for every sound: on %v, threshold %v", a.cfg.MuteMicWhilePlaying, a.cfg.MuteMicMinSeconds)
	}

	// Again: nothing changes, and a key Resound has already says nothing.
	r = a.applySoundux(p)
	if r.favorites != 0 || r.volumes != 0 || r.sorted != 0 || r.keys != 0 || len(r.notes) != 1 || len(a.globalKeys(fnPTT)) != 2 {
		t.Errorf("a second import: %+v, push-to-talk %v", r, a.globalKeys(fnPTT))
	}
}

func TestApplySounduxPartsOff(t *testing.T) {
	a, root := sounduxApp(t)
	before := a.cfg
	p := sounduxPlan(root)
	a.soundux.plan = p
	for _, pt := range []sxPart{sxFolders, sxFavorites, sxVolumes, sxKeys, sxDefaults, sxOptions} {
		a.toggleSx(pt)
	}
	r := a.applySoundux(p)
	if r.favorites+r.volumes+r.sorted+r.keys != 0 || len(r.settings) != 0 {
		t.Errorf("carried over with every part off: %+v", r)
	}
	if s, _, _ := a.lib.Sound(sxID(t, a, root, `A\a.mp3`)); s.Favorite {
		t.Error("a favorite was carried over")
	}
	if len(a.globalKeys(fnPTT)) != 1 || a.cfg.MonitorVolume != before.MonitorVolume || a.cfg.MuteMicWhilePlaying != before.MuteMicWhilePlaying {
		t.Error("settings changed")
	}
}

func TestSounduxReachable(t *testing.T) {
	a, root := sounduxApp(t)
	a.cfg.Folders = nil // a first start: nothing in the library yet
	a.soundux.plan = sounduxPlan(root)
	if n := a.sxReachable(); n != 2 {
		t.Errorf("with the folders: %d", n)
	}
	a.toggleSx(sxFolders)
	if n := a.sxReachable(); n != 0 {
		t.Errorf("without the folders: %d", n)
	}
}
