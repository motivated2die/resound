package app

import (
	"testing"

	"github.com/motivated2die/resound/internal/routing"
	"github.com/motivated2die/resound/internal/vbcable"
)

func TestApplyCableHealth(t *testing.T) {
	restart := cableInstall{state: cableRestart}
	detected := cableInstall{state: cableRestart, detected: true}
	for _, c := range []struct {
		name    string
		cable   cableInstall
		stuck   bool
		health  vbcable.Health
		changed bool
		state   cableState
		stuckTo bool
	}{
		{"missing stays idle", cableInstall{}, false, vbcable.HealthMissing, false, cableIdle, false},
		{"working stays idle", cableInstall{}, false, vbcable.HealthReady, false, cableIdle, false},
		{"found waiting", cableInstall{}, false, vbcable.HealthPending, true, cableRestart, false},
		{"found just removed", cableInstall{}, false, vbcable.HealthRemoved, true, cableRestart, false},
		{"found waiting, then it works", detected, false, vbcable.HealthReady, true, cableIdle, false},
		{"found removed, then all gone", detected, false, vbcable.HealthMissing, true, cableIdle, false},
		{"still waiting", detected, false, vbcable.HealthPending, false, cableRestart, false},
		{"this install's restart stays until it's dealt with", restart, false, vbcable.HealthReady, false, cableRestart, false},
		{"a restart that didn't help isn't asked for again", cableInstall{}, true, vbcable.HealthPending, false, cableIdle, true},
		{"working again ends that", cableInstall{}, true, vbcable.HealthReady, false, cableIdle, false},
	} {
		a := &App{cable: c.cable, cableStuck: c.stuck, cableHealth: c.health}
		if got := a.applyCableHealth(); got != c.changed {
			t.Errorf("%s: changed = %v, want %v", c.name, got, c.changed)
		}
		if a.cable.state != c.state || a.cableStuck != c.stuckTo {
			t.Errorf("%s: state %d stuck %v, want %d %v", c.name, a.cable.state, a.cableStuck, c.state, c.stuckTo)
		}
	}
}

func TestCableRateRows(t *testing.T) {
	in, out := &routing.Endpoint{ID: "in"}, &routing.Input{ID: "out"}
	clash := routing.Status{CableInput: in, CableOutput: out, CableInputRate: 48000, CableOutputRate: 44100}
	if rows := (&App{route: clash}).cableRateRows(); len(rows) != 1 {
		t.Errorf("a clash shows %d rows", len(rows))
	}
	if rows := (&App{route: clash, cable: cableInstall{state: cableChecking}}).cableRateRows(); rows != nil {
		t.Errorf("shown while the install is busy: %v", rows)
	}
	same := routing.Status{CableInput: in, CableOutput: out, CableInputRate: 44100, CableOutputRate: 44100}
	if rows := (&App{route: same}).cableRateRows(); rows != nil {
		t.Errorf("equal rates show %v", rows)
	}
}

// The panel offers each way out only where it can help.
func TestCableRestartRows(t *testing.T) {
	count := func(h vbcable.Health, wakeFailed bool) int {
		a := &App{cableHealth: h, cable: cableInstall{state: cableRestart, wakeFailed: wakeFailed}}
		return len(a.cableRestartRows())
	}
	base := count(vbcable.HealthRemoved, false)
	if got := count(vbcable.HealthPending, false); got != base+1 {
		t.Errorf("waiting device: %d rows, want the wake's hint too (%d)", got, base+1)
	}
	if got := count(vbcable.HealthPending, true); got != base {
		t.Errorf("wake failed: %d rows, want no second try (%d)", got, base)
	}
	if got := count(vbcable.HealthReady, false); got != base {
		t.Errorf("working device: %d rows, want no wake (%d)", got, base)
	}
}
