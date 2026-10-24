package input

import (
	"os"
	"testing"
)

// RunningApps lists every program, this test's own included; WindowApps
// only those with a window, never this process.
func TestRunningApps(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	self := AppName(exe)
	if !RunningApps()[self] {
		t.Errorf("RunningApps lacks this test (%s)", self)
	}
	for _, a := range WindowApps() {
		t.Logf("%s (%s)", a.Name, a.Exe)
		if a.Exe == self || a.Exe == "" || a.Name == "" {
			t.Errorf("WindowApps has %+v", a)
		}
	}
}
