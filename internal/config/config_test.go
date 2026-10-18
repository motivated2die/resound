package config

import (
	"os"
	"path/filepath"
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
