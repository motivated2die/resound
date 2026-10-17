package app

import (
	"testing"

	"github.com/motivated2die/resound/internal/audio"
)

// TestOutputErrorFollowsChoice checks that the error under the flow shows
// while the chosen device fails and goes once another choice works (here:
// the output turned off, which needs no audio device on the test machine).
func TestOutputErrorFollowsChoice(t *testing.T) {
	a := &App{engine: audio.NewEngine()}
	defer a.engine.Close()
	a.playback.lastError = "a test play failed"

	a.setOutput(audio.RoleMonitor, "{0.0.0.00000000}.{no-such-device}", true)
	if n := len(a.outputErrorRows()); n != 1 {
		t.Fatalf("failing device: %d error rows, want 1", n)
	}
	if a.playback.lastError != "" {
		t.Error("an old test play's error survived choosing a device")
	}
	for _, s := range a.engine.Status() {
		if s.Role == audio.RoleMonitor && audio.Problem(s.Err) != "the device was unplugged or turned off" {
			t.Errorf("problem = %q", audio.Problem(s.Err))
		}
	}

	a.setOutput(audio.RoleMonitor, "", false)
	if n := len(a.outputErrorRows()); n != 0 {
		t.Fatalf("after another choice: %d error rows, want 0", n)
	}
}
