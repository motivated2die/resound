package input

import (
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
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
	expect(&HotkeyEvent{ActionPause, true, 0})
	sendF24(t, false, false)
	expect(&HotkeyEvent{ActionPause, false, 0})

	if _, err := StartWatcher(Bindings{}, func(HotkeyEvent) {}); err == nil {
		t.Error("a second watcher started")
	}
}

// observer is a second pair of low-level hooks, installed before the
// watcher's, so Windows calls them after it: what the watcher swallows
// never reaches them, as it never reaches a game.
type observer struct {
	keys, wheels chan uint32
	threadID     uint32
}

var obs *observer

var obsKeyCallback = windows.NewCallback(func(code, wparam uintptr, k *kbdLLHookStruct) uintptr {
	if int32(code) >= 0 && obs != nil && (wparam == wmKeyDown || wparam == wmSysKeyDown) {
		obs.keys <- k.VKCode
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wparam, uintptr(unsafe.Pointer(k)))
	return r
})

var obsMouseCallback = windows.NewCallback(func(code, wparam uintptr, m *msLLHookStruct) uintptr {
	if int32(code) >= 0 && obs != nil && wparam == wmMouseWheel {
		obs.wheels <- m.MouseData
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wparam, uintptr(unsafe.Pointer(m)))
	return r
})

func startObserver(t *testing.T) {
	obs = &observer{keys: make(chan uint32, 16), wheels: make(chan uint32, 16)}
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		module, _, _ := procGetModuleHandleW.Call(0)
		kh, _, _ := procSetWindowsHookExW.Call(whKeyboardLL, obsKeyCallback, module, 0)
		mh, _, _ := procSetWindowsHookExW.Call(whMouseLL, obsMouseCallback, module, 0)
		obs.threadID = windows.GetCurrentThreadId()
		close(ready)
		var msg [48]byte
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(r) <= 0 {
				break
			}
		}
		procUnhookWindowsHookEx.Call(kh)
		procUnhookWindowsHookEx.Call(mh)
	}()
	<-ready
	t.Cleanup(func() { procPostThreadMessageW.Call(uintptr(obs.threadID), wmQuit, 0, 0) })
}

// TestOverlayHookLive checks with real input that the overlay key, and Esc
// and the wheel while the overlay is open, are kept from other programs,
// and that other keys aren't. It injects F23/F24 (keys no keyboard has),
// one Esc and one wheel notch, all swallowed when it passes:
//
//	$env:RESOUND_HOOK_TEST="1"; go test -run OverlayHookLive -v ./internal/input
func TestOverlayHookLive(t *testing.T) {
	if os.Getenv("RESOUND_HOOK_TEST") != "1" {
		t.Skip("set RESOUND_HOOK_TEST=1 to inject input into the desktop")
	}
	startObserver(t)
	events := make(chan HotkeyEvent, 16)
	w, err := StartWatcher(Bindings{Overlay: Combo{0x87}}, func(ev HotkeyEvent) { events <- ev })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	key := func(vk uint16, down bool) {
		in := keyboardInput{Type: inputKeyboard, VK: vk}
		if !down {
			in.Flags = keyeventfKeyUp
		}
		if err := sendInput(unsafe.Pointer(&in), unsafe.Sizeof(in)); err != nil {
			t.Fatal(err)
		}
	}
	wheel := func(delta int32) {
		in := mouseInput{Type: inputMouse, MouseData: uint32(delta), Flags: 0x0800 /* MOUSEEVENTF_WHEEL */}
		if err := sendInput(unsafe.Pointer(&in), unsafe.Sizeof(in)); err != nil {
			t.Fatal(err)
		}
	}
	expectEvent := func(want Action) HotkeyEvent {
		t.Helper()
		select {
		case ev := <-events:
			if ev.Action != want {
				t.Fatalf("event %v, want %v", ev.Action, want)
			}
			return ev
		case <-time.After(time.Second):
			t.Fatalf("no %v event", want)
		}
		return HotkeyEvent{}
	}
	expectSeen := func(ch chan uint32, seen bool, what string) {
		t.Helper()
		select {
		case v := <-ch:
			if !seen {
				t.Fatalf("%s reached other programs (%#x)", what, v)
			}
		case <-time.After(300 * time.Millisecond):
			if seen {
				t.Fatalf("%s didn't reach other programs", what)
			}
		}
	}

	key(0x86, true) // F23: not the overlay's
	key(0x86, false)
	expectSeen(obs.keys, true, "F23")

	key(0x87, true) // F24 opens
	key(0x87, false)
	expectEvent(ActionOverlayOpen)
	expectSeen(obs.keys, false, "the overlay key")

	time.Sleep(100 * time.Millisecond) // the mouse hook is installed by the hook thread's loop
	wheel(-120)
	if ev := expectEvent(ActionOverlayScroll); ev.Steps != 1 {
		t.Fatalf("one notch down: %d steps", ev.Steps)
	}
	expectSeen(obs.wheels, false, "the wheel while open")

	key(0x86, true) // other keys pass while open
	key(0x86, false)
	expectSeen(obs.keys, true, "F23 while open")

	key(vkEscape, true)
	key(vkEscape, false)
	expectEvent(ActionOverlayClose)
	expectSeen(obs.keys, false, "Esc while open")
}
