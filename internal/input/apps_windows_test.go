package input

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
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

// The taskbar (focused by a click on the tray icon before the panel opens)
// and Alt+Tab's switcher keep the app in front; Resound's own windows too.
func TestShellKeepsApp(t *testing.T) {
	find := user32.NewProc("FindWindowW")
	for _, class := range []string{"Shell_TrayWnd", "XamlExplorerHostIslandWindow"} {
		c, _ := windows.UTF16PtrFromString(class)
		h, _, _ := find.Call(uintptr(unsafe.Pointer(c)), 0)
		if h == 0 {
			t.Logf("%s: not on this PC", class)
			continue
		}
		if app, keep := appOfWindow(h); !keep {
			t.Errorf("%s (%s) counts as an app in front", class, app)
		}
	}
}
