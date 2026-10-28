package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// New configs start with muting off and a 5 s threshold; saved configs
// keep their choice and, from before the threshold, get 5 s.
func TestMuteDefaults(t *testing.T) {
	if d := Default(); d.MuteMicWhilePlaying || d.MuteMicMinSeconds != 5 {
		t.Errorf("Default: mute %v, threshold %v", d.MuteMicWhilePlaying, d.MuteMicMinSeconds)
	}

	t.Setenv("LOCALAPPDATA", t.TempDir())
	write := func(json string) {
		t.Helper()
		if err := os.MkdirAll(Dir(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(Dir(), "config.json"), []byte(json), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(`{"muteMicWhilePlaying": true, "volumeScale": 1}`) // before the threshold existed
	if c := Load(); !c.MuteMicWhilePlaying || c.MuteMicMinSeconds != 5 {
		t.Errorf("older config: mute %v, threshold %v", c.MuteMicWhilePlaying, c.MuteMicMinSeconds)
	}
	write(`{"muteMicWhilePlaying": true, "muteMicMinSeconds": 0}`)
	if c := Load(); c.MuteMicMinSeconds != 0 {
		t.Errorf("always: threshold %v", c.MuteMicMinSeconds)
	}
	write(`{"muteMicMinSeconds": 0.4}`) // under a second reads "always"
	if c := Load(); c.MuteMicMinSeconds != 0 {
		t.Errorf("0.4 s: threshold %v", c.MuteMicMinSeconds)
	}
}

// The panel's position is kept across saves, negative coordinates (a screen
// left of the main one) included; configs without one dock the panel.
func TestPanelPos(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if c := Load(); c.PanelPos != nil {
		t.Errorf("new config: %+v", c.PanelPos)
	}
	cfg := Default()
	cfg.PanelPos = &PanelPos{X: -1700, Y: 220}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if c := Load(); c.PanelPos == nil || *c.PanelPos != (PanelPos{X: -1700, Y: 220}) {
		t.Errorf("saved -1700,220, loaded %+v", c.PanelPos)
	}
	cfg.PanelPos = nil
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(Dir(), "config.json"))
	if c := Load(); c.PanelPos != nil || strings.Contains(string(data), "panelPos") {
		t.Errorf("reset: %+v, file %s", c.PanelPos, data)
	}
}

// Keybinds are tidied on load; configs from before app keybinds and further
// push-to-talk keys load unchanged.
func TestKeybindsLoad(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(json string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(Dir(), "config.json"), []byte(json), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"pushToTalkKeys": [19], "pauseHotkeys": [[71], []], "stopHotkeys": [[88]]}`)
	c := Load()
	if len(c.PushToTalkKeys) != 1 || len(c.PushToTalkExtra) != 0 || len(c.PauseHotkeys) != 1 || len(c.StopHotkeys) != 1 || c.AppKeybinds != nil {
		t.Errorf("older config: %+v", c)
	}

	write(`{"pushToTalkKeys": [], "pushToTalkExtra": [[145], [96]], "togglePauseHotkeys": [[], [120]],
		"appKeybinds": [
			{"exe": " Game.EXE ", "name": "Game", "pause": {"off": true, "keys": [[1]]}, "stop": {"keys": []}, "pushToTalk": {"keys": [[5]]}, "togglePause": {"keys": [[], [121]]},
				"overlay": {"keys": [[119], [120]]}, "launchpad": {"keys": [[20], [21]]}},
			{"exe": "game.exe", "name": "twice"},
			{"exe": "", "name": "nameless"}
		]}`)
	c = Load()
	if len(c.PushToTalkKeys) != 1 || c.PushToTalkKeys[0] != 145 || len(c.PushToTalkExtra) != 1 {
		t.Errorf("extra push-to-talk keys didn't move up: %v %v", c.PushToTalkKeys, c.PushToTalkExtra)
	}
	if len(c.TogglePauseHotkeys) != 1 || c.TogglePauseHotkeys[0][0] != 120 {
		t.Errorf("toggle pause keys: %v", c.TogglePauseHotkeys)
	}
	if len(c.AppKeybinds) != 1 {
		t.Fatalf("apps: %+v", c.AppKeybinds)
	}
	a := c.AppKeybinds[0]
	switch {
	case a.Exe != "game.exe":
		t.Errorf("exe %q", a.Exe)
	case a.Pause == nil || !a.Pause.Off || a.Pause.Keys != nil:
		t.Errorf("pause %+v, want off without keys", a.Pause)
	case a.TogglePause == nil || len(a.TogglePause.Keys) != 1 || a.TogglePause.Keys[0][0] != 121:
		t.Errorf("toggle pause %+v, want the empty combination gone", a.TogglePause)
	case a.Stop != nil:
		t.Errorf("stop %+v, want no override", a.Stop)
	case a.PushToTalk == nil || len(a.PushToTalk.Keys) != 1:
		t.Errorf("push-to-talk %+v", a.PushToTalk)
	case a.Overlay == nil || len(a.Overlay.Keys) != 1 || a.Overlay.Keys[0][0] != 119:
		t.Errorf("overlay %+v, want its first key only", a.Overlay)
	case a.Launchpad == nil || len(a.Launchpad.Keys) != 1 || a.Launchpad.Keys[0][0] != 20:
		t.Errorf("launchpad %+v, want its first key only", a.Launchpad)
	}
}
