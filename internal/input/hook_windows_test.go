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
	all          chan seenKey // F21–F24 events, presses and releases
	threadID     uint32
}

type seenKey struct {
	vk   uint32
	up   bool
	ours bool // carries sendMarker
}

var obs *observer

var obsKeyCallback = windows.NewCallback(func(code, wparam uintptr, k *kbdLLHookStruct) uintptr {
	if int32(code) >= 0 && obs != nil {
		if wparam == wmKeyDown || wparam == wmSysKeyDown {
			obs.keys <- k.VKCode
		}
		if k.VKCode >= 0x84 && k.VKCode <= 0x87 { // F21–F24: real keyboards have none
			select {
			case obs.all <- seenKey{k.VKCode, k.Flags&llkhfUp != 0, k.ExtraInfo == sendMarker}:
			default:
			}
		}
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
	obs = &observer{keys: make(chan uint32, 16), wheels: make(chan uint32, 16), all: make(chan seenKey, 32)}
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
// two Esc presses (one held) and one wheel notch, all swallowed when it
// passes:
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

	key(vkEscape, true) // a tap closes
	key(vkEscape, false)
	expectEvent(ActionOverlayEscDown)
	expectEvent(ActionOverlayClose)
	expectSeen(obs.keys, false, "Esc while open")

	key(0x87, true) // open again, then hold Esc: stop
	key(0x87, false)
	expectEvent(ActionOverlayOpen)
	start := time.Now()
	key(vkEscape, true)
	expectEvent(ActionOverlayEscDown)
	expectEvent(ActionOverlayStop)
	if held := time.Since(start); held < EscHoldTime-20*time.Millisecond || held > EscHoldTime+200*time.Millisecond {
		t.Errorf("stopped after %v, want about %v", held, EscHoldTime)
	}
	key(vkEscape, false)
	expectSeen(obs.keys, false, "the held Esc")
	select {
	case ev := <-events:
		t.Fatalf("event after the release: %v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestHandOverLive checks with real input that a pause or stop key, while
// push-to-talk keys are lent to the hook, reaches other programs only after
// the push-to-talk release, and as it was pressed otherwise. F23 stands in
// for push-to-talk, F24 for a pause key and F21 for a stop key (some
// keyboard tools take F22 for themselves):
//
//	$env:RESOUND_HOOK_TEST="1"; go test -run HandOverLive -v ./internal/input
func TestHandOverLive(t *testing.T) {
	if os.Getenv("RESOUND_HOOK_TEST") != "1" {
		t.Skip("set RESOUND_HOOK_TEST=1 to inject input into the desktop")
	}
	const ptt, pause, stop = 0x86, 0x87, 0x84
	startObserver(t)
	events := make(chan HotkeyEvent, 16)
	w, err := StartWatcher(Bindings{Pause: []Combo{{pause}}, Stop: []Combo{{stop}}}, func(ev HotkeyEvent) { events <- ev })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	user := func(vk uint16, down bool) { // a key as the user presses it
		in := keyboardInput{Type: inputKeyboard, VK: vk}
		if !down {
			in.Flags = keyeventfKeyUp
		}
		if err := sendInput(unsafe.Pointer(&in), unsafe.Sizeof(in)); err != nil {
			t.Fatal(err)
		}
	}
	expectSeen := func(want ...seenKey) {
		t.Helper()
		for _, k := range want {
			select {
			case got := <-obs.all:
				if got != k {
					t.Fatalf("other programs got %+v, want %+v", got, k)
				}
			case <-time.After(time.Second):
				t.Fatalf("other programs didn't get %+v", k)
			}
		}
		select {
		case got := <-obs.all:
			t.Fatalf("other programs got %+v as well", got)
		case <-time.After(150 * time.Millisecond):
		}
	}
	expectEvents := func(want ...HotkeyEvent) {
		t.Helper()
		for _, ev := range want {
			select {
			case got := <-events:
				if got != ev {
					t.Fatalf("event %v, want %v", got, ev)
				}
			case <-time.After(time.Second):
				t.Fatalf("no %v event", ev)
			}
		}
	}

	for _, key := range []uint16{pause, stop} {
		start := HotkeyEvent{Action: ActionPause, Down: true}
		if key == stop {
			start.Action = ActionStop
		}
		if err := Press(Combo{ptt}); err != nil {
			t.Fatal(err)
		}
		expectSeen(seenKey{ptt, false, true})
		w.LendPTT(Combo{ptt})

		user(key, true) // the release first, then the key
		expectSeen(seenKey{ptt, true, true}, seenKey{uint32(key), false, true})
		expectEvents(start, HotkeyEvent{Action: ActionHandOver, Down: true})
		if c := w.TakePTT(); c != nil {
			t.Fatalf("the keys are still lent after the hand-over: %v", c)
		}
		user(key, true) // a repeat passes
		user(key, false)
		expectSeen(seenKey{uint32(key), false, false}, seenKey{uint32(key), true, false})
		if key == pause {
			expectEvents(HotkeyEvent{Action: ActionPause})
		}

		user(key, true) // nothing lent: the key passes as it is
		user(key, false)
		expectSeen(seenKey{uint32(key), false, false}, seenKey{uint32(key), true, false})
		expectEvents(start)
		if key == pause {
			expectEvents(HotkeyEvent{Action: ActionPause})
		}
	}

	// A tap sent in one go: its release reaches the hook before the press
	// is passed on, and must still come after it.
	if err := Press(Combo{ptt}); err != nil {
		t.Fatal(err)
	}
	expectSeen(seenKey{ptt, false, true})
	w.LendPTT(Combo{ptt})
	tap := []inputRecord{
		asRecord(keyboardInput{Type: inputKeyboard, VK: pause}),
		asRecord(keyboardInput{Type: inputKeyboard, VK: pause, Flags: keyeventfKeyUp}),
	}
	if _, err := sendRecordsN(tap); err != nil {
		t.Fatal(err)
	}
	expectSeen(seenKey{ptt, true, true}, seenKey{pause, false, true}, seenKey{pause, true, true})
	expectEvents(HotkeyEvent{Action: ActionPause, Down: true}, HotkeyEvent{Action: ActionPause}, HotkeyEvent{Action: ActionHandOver, Down: true})

	select {
	case ev := <-events:
		t.Fatalf("unexpected event %v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}
