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
	w, err := StartWatcher(Keymap{Global: Bindings{Pause: []Combo{{0x87}}}}, func(ev HotkeyEvent) { events <- ev })
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
	expect(&HotkeyEvent{Action: ActionPause, Down: true})
	sendF24(t, false, false)
	expect(&HotkeyEvent{Action: ActionPause, Down: false})

	if _, err := StartWatcher(Keymap{Global: Bindings{}}, func(HotkeyEvent) {}); err == nil {
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
	if vk, down, ok := mouseButton(wparam, m.MouseData); int32(code) >= 0 && obs != nil && ok && vk == VKXButton2 {
		select {
		case obs.all <- seenKey{uint32(vk), !down, m.ExtraInfo == sendMarker}:
		default:
		}
		return 1 // the test's "forward" clicks never reach the window under the pointer
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
	w, err := StartWatcher(Keymap{Global: Bindings{Overlay: Combo{0x87}}}, func(ev HotkeyEvent) { events <- ev })
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

// TestLaunchpadHookLive checks with real input that the launchpad lasts
// while its key is held, also through the checks for lost releases (Windows
// never counts a swallowed key as down), that it keeps keys and the wheel
// from other programs meanwhile, and that letting go ends it. F24 is the
// launchpad key, F23 a key pressed while it is held; one wheel notch:
//
//	$env:RESOUND_HOOK_TEST="1"; go test -run LaunchpadHookLive -v ./internal/input
func TestLaunchpadHookLive(t *testing.T) {
	if os.Getenv("RESOUND_HOOK_TEST") != "1" {
		t.Skip("set RESOUND_HOOK_TEST=1 to inject input into the desktop")
	}
	startObserver(t)
	events := make(chan HotkeyEvent, 16)
	w, err := StartWatcher(Keymap{Global: Bindings{Launchpad: Combo{0x87}}}, func(ev HotkeyEvent) { events <- ev })
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
	expect := func(want HotkeyEvent) {
		t.Helper()
		select {
		case ev := <-events:
			if ev != want {
				t.Fatalf("event %+v, want %+v", ev, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("no %v event", want.Action)
		}
	}
	none := func(what string, d time.Duration) {
		t.Helper()
		select {
		case ev := <-events:
			t.Fatalf("%s: event %+v", what, ev)
		case <-time.After(d):
		}
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

	key(0x87, true)
	expect(HotkeyEvent{Action: ActionLaunchpad, Down: true})
	expectSeen(obs.keys, false, "the launchpad key")
	s, _, _ := procGetAsyncKeyState.Call(0x87)
	t.Logf("GetAsyncKeyState(F24) while held and swallowed: %#x", s&0xFFFF)
	none("held through the checks for lost releases", 3*staleCheckMS*time.Millisecond)

	key(0x86, true) // F23: swallowed, no launchpad key
	key(0x86, false)
	expectSeen(obs.keys, false, "F23 while held")
	none("F23", 100*time.Millisecond)

	in := mouseInput{Type: inputMouse, MouseData: uint32(0xFFFFFF88) /* -120 */, Flags: 0x0800 /* MOUSEEVENTF_WHEEL */}
	if err := sendInput(unsafe.Pointer(&in), unsafe.Sizeof(in)); err != nil {
		t.Fatal(err)
	}
	expect(HotkeyEvent{Action: ActionLaunchpadTab, Down: true, Steps: 1})
	expectSeen(obs.wheels, false, "the wheel while held")

	key(0x87, false)
	expect(HotkeyEvent{Action: ActionLaunchpad})
	expectSeen(obs.keys, false, "the launchpad key's release")

	key(0x86, true) // keys pass again
	key(0x86, false)
	expectSeen(obs.keys, true, "F23 after")
	none("after", 200*time.Millisecond)
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
	w, err := StartWatcher(Keymap{Global: Bindings{Pause: []Combo{{pause}}, Stop: []Combo{{stop}}}}, func(ev HotkeyEvent) { events <- ev })
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

// TestMouseHandOverLive is TestHandOverLive with a mouse button as the
// pause key: Mouse 5 (forward) while push-to-talk (F23) is lent reaches
// other programs only after the push-to-talk release. The observer swallows
// every Mouse 5 event, so the forward clicks reach no window:
//
//	$env:RESOUND_HOOK_TEST="1"; go test -run MouseHandOverLive -v ./internal/input
func TestMouseHandOverLive(t *testing.T) {
	if os.Getenv("RESOUND_HOOK_TEST") != "1" {
		t.Skip("set RESOUND_HOOK_TEST=1 to inject input into the desktop")
	}
	const ptt = 0x86
	startObserver(t)
	events := make(chan HotkeyEvent, 16)
	w, err := StartWatcher(Keymap{Global: Bindings{Pause: []Combo{{VKXButton2}}}}, func(ev HotkeyEvent) {
		if ev.Action != ActionApp {
			events <- ev
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	time.Sleep(100 * time.Millisecond) // the hook thread installs the mouse hook
	button := func(down bool) {        // the button as the user presses it
		in := mouseInput{Type: inputMouse, MouseData: 2 /* XBUTTON2 */, Flags: mouseeventfXUp}
		if down {
			in.Flags = mouseeventfXDown
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

	if err := Press(Combo{ptt}); err != nil {
		t.Fatal(err)
	}
	expectSeen(seenKey{ptt, false, true})
	w.LendPTT(Combo{ptt})
	button(true) // the release first, then the button
	expectSeen(seenKey{ptt, true, true}, seenKey{VKXButton2, false, true})
	expectEvents(HotkeyEvent{Action: ActionPause, Down: true}, HotkeyEvent{Action: ActionHandOver, Down: true})
	button(false)
	expectSeen(seenKey{VKXButton2, true, false})
	expectEvents(HotkeyEvent{Action: ActionPause})

	button(true) // nothing lent: the button passes as it is
	button(false)
	expectSeen(seenKey{VKXButton2, false, false}, seenKey{VKXButton2, true, false})
	expectEvents(HotkeyEvent{Action: ActionPause, Down: true}, HotkeyEvent{Action: ActionPause})

	// A click sent in one go: its release must still come after the press.
	if err := Press(Combo{ptt}); err != nil {
		t.Fatal(err)
	}
	expectSeen(seenKey{ptt, false, true})
	w.LendPTT(Combo{ptt})
	click := []inputRecord{
		asRecord(mouseInput{Type: inputMouse, MouseData: 2, Flags: mouseeventfXDown}),
		asRecord(mouseInput{Type: inputMouse, MouseData: 2, Flags: mouseeventfXUp}),
	}
	if _, err := sendRecordsN(click); err != nil {
		t.Fatal(err)
	}
	expectSeen(seenKey{ptt, true, true}, seenKey{VKXButton2, false, true}, seenKey{VKXButton2, true, true})
	expectEvents(HotkeyEvent{Action: ActionPause, Down: true}, HotkeyEvent{Action: ActionPause}, HotkeyEvent{Action: ActionHandOver, Down: true})
}

// TestTalkLive checks with real input that the user's own push-to-talk key
// (F23) reaches other programs as pressed and reports a talk, Resound's own
// press of it none, and that a pause key (F24) pressed while the user talks
// passes as it is, without letting go of the push-to-talk keys lent:
//
//	$env:RESOUND_HOOK_TEST="1"; go test -run TalkLive -v ./internal/input
func TestTalkLive(t *testing.T) {
	if os.Getenv("RESOUND_HOOK_TEST") != "1" {
		t.Skip("set RESOUND_HOOK_TEST=1 to inject input into the desktop")
	}
	const ptt, pause = 0x86, 0x87
	startObserver(t)
	events := make(chan HotkeyEvent, 16)
	w, err := StartWatcher(Keymap{Global: Bindings{Talk: []Combo{{ptt}}, Pause: []Combo{{pause}}}}, func(ev HotkeyEvent) {
		if ev.Action != ActionApp {
			events <- ev
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	user := func(vk uint16, down bool) {
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
		select {
		case got := <-events:
			t.Fatalf("event %v as well", got)
		case <-time.After(100 * time.Millisecond):
		}
	}

	if err := Press(Combo{ptt}); err != nil { // Resound holds push-to-talk: no talk
		t.Fatal(err)
	}
	expectSeen(seenKey{ptt, false, true})
	expectEvents()
	w.LendPTT(Combo{ptt})

	user(ptt, true) // the user's own press passes and starts a talk
	expectSeen(seenKey{ptt, false, false})
	expectEvents(HotkeyEvent{Action: ActionTalk, Down: true})
	if w.Talking() != 0 {
		t.Fatalf("Talking() = %d while the user holds the key", w.Talking())
	}
	user(pause, true) // no hand-over while talking: no push-to-talk release
	user(pause, false)
	expectSeen(seenKey{pause, false, false}, seenKey{pause, true, false})
	expectEvents(HotkeyEvent{Action: ActionPause, Down: true}, HotkeyEvent{Action: ActionPause})
	if c := w.TakePTT(); c == nil {
		t.Fatal("the loan was used while the user talked")
	}
	user(ptt, false)
	expectSeen(seenKey{ptt, true, false})
	expectEvents(HotkeyEvent{Action: ActionTalk})
	if w.Talking() != -1 {
		t.Fatalf("Talking() = %d after the release", w.Talking())
	}
	if err := Release(Combo{ptt}); err != nil {
		t.Fatal(err)
	}
	expectSeen(seenKey{ptt, true, true})
}
