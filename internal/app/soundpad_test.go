package app

import (
	"path/filepath"
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
		Stop:       []uint16{0x48},
		Pause:      []uint16{vkX}, // Resound's stop key
	}
	r := a.applySoundpad(p)

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
	if stop := a.globalKeys(fnStop); len(stop) != 2 || stop[1][0] != 0x48 {
		t.Errorf("stop: %v", stop)
	}
	if pause := a.globalKeys(fnPause); len(pause) != 1 || r.keys != 2 || len(r.notes) != 1 || !strings.Contains(r.notes[0], "Soundpad's pause key") {
		t.Errorf("pause %v, keys %d, notes %q", pause, r.keys, r.notes)
	}

	// Again: nothing changes.
	r = a.applySoundpad(p)
	if r.favorites+r.colors+r.keys != 0 || len(a.globalKeys(fnStop)) != 2 {
		t.Errorf("a second import: %+v", r)
	}
}

func TestApplySoundpadPartsOff(t *testing.T) {
	a, root := sounduxApp(t)
	p := soundpadPlan(root)
	a.soundpad.plan = p
	a.soundpad.settings = soundpad.Settings{Found: true, Stop: []uint16{0x48}}
	for _, pt := range []spPart{spFavorites, spColors, spKeys} {
		a.toggleSp(pt)
	}
	if r := a.applySoundpad(p); r.favorites+r.colors+r.keys != 0 {
		t.Errorf("carried over with every part off: %+v", r)
	}
	if s, _, _ := a.lib.Sound(sxID(t, a, root, `A\a.mp3`)); s.Favorite || s.Color != "" || len(a.globalKeys(fnStop)) != 1 {
		t.Error("something carried over")
	}
}

// Folders that hold mostly other sounds start off; the user's choice wins;
// favorites reach only the sounds of folders being added or in the library.
func TestSoundpadFolders(t *testing.T) {
	a, root := sounduxApp(t)
	a.cfg.Folders = nil
	p := soundpadPlan(root)
	p.Folders[0].Files = 40 // A: 2 of 40 listed
	a.soundpad.plan = p
	if a.spFolderOn(p.Folders[0]) || !a.spFolderOn(p.Folders[1]) {
		t.Fatal("suggestions")
	}
	if n := a.spReachable(); n != 1 {
		t.Errorf("reachable with B only: %d", n)
	}
	a.toggleSpFolder(p.Folders[0])
	a.toggleSpFolder(p.Folders[1])
	if !a.spFolderOn(p.Folders[0]) || a.spFolderOn(p.Folders[1]) || a.spReachable() != 2 {
		t.Errorf("after switching: A %v B %v reachable %d", a.spFolderOn(p.Folders[0]), a.spFolderOn(p.Folders[1]), a.spReachable())
	}
}
