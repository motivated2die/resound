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
