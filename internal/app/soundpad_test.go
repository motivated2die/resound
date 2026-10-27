package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/soundpad"
)

func soundpadPlan(root string) *soundpad.Plan {
	return &soundpad.Plan{
		Folders: []soundpad.Folder{
			{Path: filepath.Join(root, "A"), Listed: 2, Files: 3},
			{Path: filepath.Join(root, "B"), Listed: 1, Files: 1},
		},
		Sounds: []soundpad.Sound{
			{Path: filepath.Join(root, `A\a.mp3`), Hotbar: true, Color: "red"},
			{Path: filepath.Join(root, `A\b.mp3`), Color: "teal"},
			{Path: filepath.Join(root, `B\d.mp3`), Hotbar: true},
			{Path: filepath.Join(root, `Elsewhere\e.mp3`), Hotbar: true, Color: "pink"},
		},
		Sizes: []int64{1, 1, 1, 1},
	}
}

func TestApplySoundpad(t *testing.T) {
	a, root := sounduxApp(t)
	a.lib.Update(sxID(t, a, root, `A\b.mp3`), func(s *library.SoundSettings) { s.Color = "blue" })
	p := soundpadPlan(root)
	a.soundpad.plan = p
	a.soundpad.settings = soundpad.Settings{
		Found:      true,
		PushToTalk: [][]uint16{{vkV}},
		Stop:       []uint16{0xA4, 0x48}, // Alt+H
		Pause:      []uint16{vkX},        // Resound's stop key
		MuteMic:    true,
	}
	a.cfg.MuteMicMinSeconds = 3
	r := a.applySoundpad(spOriginals(p))

	snd := func(rel string) library.Sound {
		s, _, _ := a.lib.Sound(sxID(t, a, root, rel))
		return s
	}
	if s := snd(`A\a.mp3`); !s.Favorite || s.Color != "red" {
		t.Errorf("a.mp3: %+v", s.SoundSettings)
	}
	if s := snd(`A\b.mp3`); s.Favorite || s.Color != "blue" {
		t.Errorf("b.mp3 must keep the color set in Resound: %+v", s.SoundSettings)
	}
	if s := snd(`B\d.mp3`); !s.Favorite {
		t.Errorf("d.mp3: %+v", s.SoundSettings)
	}
	if r.favorites != 2 || r.colors != 1 {
		t.Errorf("favorites %d colors %d; e.mp3 isn't in the library", r.favorites, r.colors)
	}
	if ptt := a.globalKeys(fnPTT); len(ptt) != 2 || ptt[1][0] != vkV || !a.cfg.PushToTalkWhilePlaying {
		t.Errorf("push-to-talk: %v", ptt)
	}
	if stop := a.globalKeys(fnStop); len(stop) != 2 || fmt.Sprint(stop[1]) != "[164 72]" {
		t.Errorf("stop: %v", stop)
	}
	if pause := a.globalKeys(fnPause); len(pause) != 1 || r.keys != 2 || len(r.notes) != 1 || !strings.Contains(r.notes[0], "Soundpad's pause key") {
		t.Errorf("pause %v, keys %d, notes %q", pause, r.keys, r.notes)
	}

	if !r.muteMic || !a.cfg.MuteMicWhilePlaying || a.cfg.MuteMicMinSeconds != 0 {
		t.Errorf("mute mic: %v, on %v, from %v s; want always", r.muteMic, a.cfg.MuteMicWhilePlaying, a.cfg.MuteMicMinSeconds)
	}

	// Again: nothing changes.
	r = a.applySoundpad(spOriginals(p))
	if r.favorites+r.colors+r.keys != 0 || len(a.globalKeys(fnStop)) != 2 {
		t.Errorf("a second import: %+v", r)
	}
}

func TestApplySoundpadPartsOff(t *testing.T) {
	a, root := sounduxApp(t)
	p := soundpadPlan(root)
	a.soundpad.plan = p
	a.soundpad.settings = soundpad.Settings{Found: true, Stop: []uint16{0x48}, MuteMic: true}
	for _, pt := range []spPart{spFavorites, spColors, spKeys, spMuteMic} {
		a.toggleSp(pt)
	}
	if r := a.applySoundpad(spOriginals(p)); r.favorites+r.colors+r.keys != 0 || r.muteMic || a.cfg.MuteMicWhilePlaying {
		t.Errorf("carried over with every part off: %+v", r)
	}
	if s, _, _ := a.lib.Sound(sxID(t, a, root, `A\a.mp3`)); s.Favorite || s.Color != "" || len(a.globalKeys(fnStop)) != 1 {
		t.Error("something carried over")
	}
}

// By folder, every folder starts on, whatever else it holds; the user's
// choice wins; favorites reach only the sounds of folders being added or
// in the library.
func TestSoundpadFolders(t *testing.T) {
	a, root := sounduxApp(t)
	a.cfg.Folders = nil
	p := soundpadPlan(root)
	p.Folders[0].Files = 40 // A: 2 of 40 listed
	a.soundpad.plan = p
	if !a.spFolderOn(p.Folders[0]) || !a.spFolderOn(p.Folders[1]) || a.spReachable() != 3 {
		t.Fatalf("folders start on: A %v B %v reachable %d", a.spFolderOn(p.Folders[0]), a.spFolderOn(p.Folders[1]), a.spReachable())
	}
	a.toggleSpFolder(p.Folders[0])
	if a.spFolderOn(p.Folders[0]) || !a.spFolderOn(p.Folders[1]) || a.spReachable() != 1 {
		t.Errorf("A off: A %v B %v reachable %d", a.spFolderOn(p.Folders[0]), a.spFolderOn(p.Folders[1]), a.spReachable())
	}
}

// By category: the copies are in the library, a sound in two categories
// is a favorite once but has its color in both, the originals keep their
// settings, and each tab has Soundpad's order in every layout, once.
func TestSoundpadByCategory(t *testing.T) {
	a, root := sounduxApp(t)
	p := soundpadPlan(root)
	p.Sounds = p.Sounds[:3]
	p.Categories = []soundpad.PlanCategory{
		{Title: "Fun", Dir: "Fun", Sounds: []int{2, 0}},
		{Title: "Old", Dir: "Old", Sounds: []int{1, 0}},
	}
	a.soundpad.plan = p
	target := t.TempDir()
	c, err := soundpad.CopyByCategory(target, p, func(int) {}, func() bool { return false })
	if err != nil || c.Copied != 4 {
		t.Fatalf("copy: %+v, %v", c, err)
	}
	a.cfg.Folders = append(a.cfg.Folders, target)
	a.lib.Scan(a.cfg.Folders)

	sounds, tabs := spCopies(p, target, c)
	r := a.applySoundpad(sounds)
	snd := func(rel string) library.Sound {
		s, _, _ := a.lib.Sound(sxID(t, a, target, rel))
		return s
	}
	if s := snd(`Fun\a.mp3`); !s.Favorite || s.Color != "red" {
		t.Errorf(`Fun\a.mp3: %+v`, s.SoundSettings)
	}
	if s := snd(`Old\a.mp3`); s.Favorite || s.Color != "red" {
		t.Errorf(`Old\a.mp3 is a favorite through Fun already: %+v`, s.SoundSettings)
	}
	if s := snd(`Old\b.mp3`); s.Color != "teal" {
		t.Errorf(`Old\b.mp3: %+v`, s.SoundSettings)
	}
	if s, _, _ := a.lib.Sound(sxID(t, a, root, `A\a.mp3`)); s.Favorite || s.Color != "" {
		t.Errorf("the original changed: %+v", s.SoundSettings)
	}
	if r.favorites != 2 || r.colors != 3 {
		t.Errorf("favorites %d colors %d", r.favorites, r.colors)
	}

	for _, tab := range tabs {
		if !a.orderTab(tab.dir, tab.paths) {
			t.Errorf("%s: not ordered", tab.dir)
		}
	}
	for _, tab := range a.lib.Tabs() {
		if filepath.Base(tab.Path) != "Fun" {
			continue
		}
		lay := a.lib.Layout(strconv.FormatUint(tab.ID, 10))
		want := fmt.Sprint([]uint64{sxID(t, a, target, `Fun\d.mp3`), sxID(t, a, target, `Fun\a.mp3`)})
		for _, mode := range phoneLayouts {
			if got := fmt.Sprint(lay.Orders[mode]); got != want {
				t.Errorf("Fun's %s order: %s, want %s", mode, got, want)
			}
		}
	}
	if a.orderTab(tabs[0].dir, tabs[0].paths) {
		t.Error("a tab ordered already was ordered again")
	}
}
