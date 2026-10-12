package input

import (
	"os"
	"testing"
	"time"
	"unsafe"
)

// sendF24 injects an F24 press or release, tagged as Resound's own or not.
func sendF24(t *testing.T, down, own bool) {
	t.Helper()
	in := keyboardInput{Type: inputKeyboard, VK: 0x87}
	if !down {
		in.Flags = keyeventfKeyUp
	}
	if own {
		in.ExtraInfo = sendMarker
	}
	if err := sendInput(unsafe.Pointer(&in), unsafe.Sizeof(in)); err != nil {
		t.Fatal(err)
	}
}

// TestWatcherLive installs the real hook and injects F24 (a key no keyboard
// has). It types into the desktop, so it only runs on request:
//
//	$env:RESOUND_HOOK_TEST="1"; go test -run WatcherLive -v ./internal/input
func TestWatcherLive(t *testing.T) {
	if os.Getenv("RESOUND_HOOK_TEST") != "1" {
		t.Skip("set RESOUND_HOOK_TEST=1 to inject F24 into the desktop")
	}
	events := make(chan HotkeyEvent, 8)
	w, err := StartWatcher(Bindings{Pause: []Combo{{0x87}}}, func(ev HotkeyEvent) { events <- ev })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	expect := func(want *HotkeyEvent) {
		t.Helper()
		select {
		case got := <-events:
			if want == nil || got != *want {
				t.Fatalf("got %v, want %v", got, want)
			}
		case <-time.After(time.Second):
			if want != nil {
				t.Fatalf("no event, want %v", *want)
			}
		}
	}

	sendF24(t, true, true) // Resound's own press: ignored
	sendF24(t, false, true)
	expect(nil)

	sendF24(t, true, false)
	expect(&HotkeyEvent{ActionPause, true})
	sendF24(t, false, false)
	expect(&HotkeyEvent{ActionPause, false})

	if _, err := StartWatcher(Bindings{}, func(HotkeyEvent) {}); err == nil {
		t.Error("a second watcher started")
	}
}
