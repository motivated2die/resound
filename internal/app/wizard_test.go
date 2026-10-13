package app

import (
	"testing"

	"github.com/motivated2die/resound/internal/config"
)

func TestMigrateSetupStep(t *testing.T) {
	for _, c := range []struct {
		step     int
		complete bool
		want     int
	}{
		{0, false, 0}, // welcome
		{1, false, 1}, // sounds
		{2, false, 2}, // speakers → audio
		{3, false, 2}, // voice chat → audio
		{4, false, 3}, // push-to-talk
		{5, false, 4}, // phone
		{0, true, 0},  // finished: nothing to move
	} {
		cfg := config.Config{SetupStep: c.step, SetupComplete: c.complete}
		if !migrateSetupStep(&cfg) || cfg.SetupStep != c.want || cfg.SetupFlow != setupFlow {
			t.Errorf("step %d: got step %d flow %d, want %d", c.step, cfg.SetupStep, cfg.SetupFlow, c.want)
		}
		if migrateSetupStep(&cfg) || cfg.SetupStep != c.want {
			t.Errorf("step %d: migrated twice", c.step)
		}
	}
}
