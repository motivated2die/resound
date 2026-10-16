package input

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Watcher watches the keyboard for hotkeys with a low-level keyboard hook
// (WH_KEYBOARD_LL) on its own thread. For pause and stop keys the hook only
// observes: every key still reaches the game, which matters because a pause
// key is usually the game's push-to-talk for another channel.
// RegisterHotKey would swallow the key instead.
//
// The overlay is the exception (Filter): its key, and Esc and the mouse
// wheel while it is open, are kept from the game. The wheel needs a
// low-level mouse hook (WH_MOUSE_LL), installed only while the overlay is
// open. Swallowing in a low-level hook keeps the input from every program,
// raw input (most games) included.
//
// Pause and stop keys are held back for a moment only while the app has
// lent the hook the push-to-talk keys it holds (LendPTT): the key that
// starts a pause or a stop is swallowed, and the hook thread sends the
// push-to-talk release followed by that key in one SendInput call. A game
// then sees push-to-talk end before the user's key goes down; otherwise
// many games let the release end the user's own push-to-talk as well.
// Repeats and the key's release pass as usual.
//
// Resound's own push-to-talk presses carry a marker (sendMarker) and are
// ignored, so they can never trigger a hotkey. Like SendInput, the hook does
// not see keys typed into a program running as administrator.
type Watcher struct {
	bindings atomic.Pointer[Bindings]
	lent     atomic.Pointer[Combo] // push-to-talk keys lent by the app (LendPTT)
	onEvent  func(HotkeyEvent)

	threadID uint32
	done     chan struct{}
	closeMu  sync.Once
}

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procSetTimer            = user32.NewProc("SetTimer")
	procKillTimer           = user32.NewProc("KillTimer")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	procGetModuleHandleW    = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14
	wmQuit       = 0x0012
	wmApp        = 0x8000
	wmMouseWheel = 0x020A

	// Thread messages to the hook thread.
	wmSyncMouse   = wmApp + 1 // install or remove the mouse hook to match the overlay
	wmSetOverlay  = wmApp + 2 // wParam 1: the app opened the overlay, 0: closed it
	wmHandOver    = wmApp + 3 // pass on the presses in handOvers
	wmTimer       = 0x0113
	wmKeyDown     = 0x0100
	wmSysKeyDown  = 0x0104
	llkhfExtended = 0x01
	llkhfUp       = 0x80

	// staleCheck is how often keys believed held are checked against the
	// real keyboard state, in case a release never reached the hook (the
	// secure desktop of Ctrl+Alt+Del or UAC, a lock screen, an elevated
	// window taking focus).
	staleCheckMS = 250

	// inFlightTime is how long a key passed on after a hand-over may take
	// to come back through the hook before releaseStale judges it.
	inFlightTime = time.Second
)

// kbdLLHookStruct mirrors KBDLLHOOKSTRUCT.
type kbdLLHookStruct struct {
	VKCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// msLLHookStruct mirrors MSLLHOOKSTRUCT.
type msLLHookStruct struct {
	Pt        struct{ X, Y int32 }
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// The hook callbacks are plain functions; they find their watcher here.
// Only one Watcher runs at a time.
var (
	activeMu sync.Mutex
	active   *hookThread
)

type hookThread struct {
	w         *Watcher
	hook      uintptr
	mouseHook uintptr // only while the overlay is open
	escTimer  uintptr // thread timer for holding Esc (EscHoldTime), 0 = none
	filter    Filter
	handOvers []handOver // swallowed presses waiting to be passed on
	// inFlight holds the keys passed on whose press hasn't come back
	// through the hook yet, with when they were swallowed. Until then
	// GetAsyncKeyState still says they're up, which releaseStale must not
	// take for a release.
	inFlight map[uint16]time.Time
	scans    map[uint16]uint16 // the scan codes of the keys passed on
}

// handOver is a swallowed press of a pause or stop key, passed on after
// letting go of the push-to-talk keys (ptt), or the release of such a key
// that came before its press was passed on (up, no ptt).
type handOver struct {
	ptt  Combo
	vk   uint16
	scan uint16 // as it arrived, with the 0xE0 prefix for extended keys
	up   bool
}

func activeThread() *hookThread {
	activeMu.Lock()
	defer activeMu.Unlock()
	return active
}

var hookCallback = windows.NewCallback(func(code, wparam uintptr, k *kbdLLHookStruct) uintptr {
	if int32(code) >= 0 {
		if t := activeThread(); t != nil && k.ExtraInfo == sendMarker {
			if k.Flags&llkhfUp == 0 {
				delete(t.inFlight, uint16(k.VKCode)) // a press passed on has arrived
			}
		} else if t != nil && k.VKCode > 0 && k.VKCode < 0xFF {
			down := (wparam == wmKeyDown || wparam == wmSysKeyDown) && k.Flags&llkhfUp == 0
			if !down && t.queueRelease(uint16(k.VKCode)) {
				events, _ := t.filter.Key(t.w.bindings.Load(), uint16(k.VKCode), false)
				t.deliver(events)
				return 1
			}
			wasOpen := t.filter.Open()
			events, swallow := t.filter.Key(t.w.bindings.Load(), uint16(k.VKCode), down)
			if !swallow && down && handsOver(events) {
				swallow = t.handOverLater(uint16(k.VKCode), k.ScanCode, k.Flags)
			}
			t.deliver(events)
			t.timeEsc(events)
			if t.filter.Open() != wasOpen {
				t.syncMouseLater()
			}
			if swallow {
				return 1
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wparam, uintptr(unsafe.Pointer(k)))
	return r
})

var mouseCallback = windows.NewCallback(func(code, wparam uintptr, m *msLLHookStruct) uintptr {
	if int32(code) >= 0 && wparam == wmMouseWheel {
		if t := activeThread(); t != nil {
			events, swallow := t.filter.Wheel(int(int16(m.MouseData >> 16)))
			t.deliver(events)
			if swallow {
				return 1
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wparam, uintptr(unsafe.Pointer(m)))
	return r
})

// handOverLater takes the lent push-to-talk keys and has the message loop
// let go of them and then pass the press on (passOn). It reports whether
// it did, i.e. whether the press must be swallowed now. Without lent keys,
// or if the message can't be posted, the press passes as it is.
func (t *hookThread) handOverLater(vk uint16, scan, flags uint32) bool {
	ptt := t.w.lent.Swap(nil)
	if ptt == nil {
		return false
	}
	sc := uint16(scan & 0xFF)
	if flags&llkhfExtended != 0 {
		sc |= 0xE000
	}
	t.handOvers = append(t.handOvers, handOver{ptt: *ptt, vk: vk, scan: sc})
	if r, _, _ := procPostThreadMessageW.Call(uintptr(t.w.threadID), wmHandOver, 0, 0); r == 0 {
		t.handOvers = t.handOvers[:len(t.handOvers)-1]
		t.w.lent.CompareAndSwap(nil, ptt)
		return false
	}
	if t.inFlight == nil {
		t.inFlight, t.scans = map[uint16]time.Time{}, map[uint16]uint16{}
	}
	t.inFlight[vk], t.scans[vk] = time.Now(), sc
	return true
}

// queueRelease has the release of vk passed on behind its press while that
// press hasn't come back through the hook yet, waiting to be sent or sent
// but queued behind this release, so a game never gets the release first.
// It reports whether it did (the release must be swallowed now).
func (t *hookThread) queueRelease(vk uint16) bool {
	waiting := false
	for _, h := range t.handOvers {
		waiting = waiting || h.vk == vk && !h.up
	}
	at, sent := t.inFlight[vk]
	if !waiting && (!sent || time.Since(at) >= inFlightTime) {
		return false
	}
	t.handOvers = append(t.handOvers, handOver{vk: vk, scan: t.scans[vk], up: true})
	if r, _, _ := procPostThreadMessageW.Call(uintptr(t.w.threadID), wmHandOver, 0, 0); r == 0 && !waiting {
		t.handOvers = t.handOvers[:len(t.handOvers)-1]
		return false
	}
	return true
}

// passOn sends, for each swallowed press, the push-to-talk release and then
// the press in one SendInput call, so no other input comes between them.
// The press is passed on whatever happens to the release: a swallowed key
// must never be lost. Hook thread, outside the callback.
func (t *hookThread) passOn() {
	for len(t.handOvers) > 0 {
		h := t.handOvers[0]
		t.handOvers = t.handOvers[1:]
		key := keyInputScan(h.vk, h.scan)
		if h.up {
			key.Flags |= keyeventfKeyUp
		}
		in := append(releaseRecords(h.ptt), asRecord(key))
		n, err := sendRecordsN(in)
		if h.up {
			continue // its press reported the hand-over
		}
		if n < len(in) {
			if n, _ := sendRecordsN(in[len(in)-1:]); n == 0 {
				delete(t.inFlight, h.vk) // lost; releaseStale may judge it
			}
		}
		t.w.onEvent(HotkeyEvent{Action: ActionHandOver, Down: err == nil})
	}
}

// timeEsc starts (or restarts) the Esc hold timer when Esc goes down in
// the open overlay, and stops it when it is let go early.
func (t *hookThread) timeEsc(events []HotkeyEvent) {
	for _, ev := range events {
		switch {
		case ev.Action == ActionOverlayEscDown:
			t.escTimer, _, _ = procSetTimer.Call(0, t.escTimer, uintptr(EscHoldTime.Milliseconds()), 0)
		case ev.Action == ActionOverlayClose && t.escTimer != 0:
			procKillTimer.Call(0, t.escTimer)
			t.escTimer = 0
		}
	}
}

// syncMouseLater has the message loop install or remove the mouse hook
// (not from inside a hook callback).
func (t *hookThread) syncMouseLater() {
	procPostThreadMessageW.Call(uintptr(t.w.threadID), wmSyncMouse, 0, 0)
}

// syncMouse installs the mouse hook while the overlay is open and removes
// it once it closes. Hook thread.
func (t *hookThread) syncMouse() {
	switch open := t.filter.Open(); {
	case open && t.mouseHook == 0:
		module, _, _ := procGetModuleHandleW.Call(0)
		t.mouseHook, _, _ = procSetWindowsHookExW.Call(whMouseLL, mouseCallback, module, 0)
	case !open && t.mouseHook != 0:
		procUnhookWindowsHookEx.Call(t.mouseHook)
		t.mouseHook = 0
	}
}

// deliver passes events on. onEvent must return quickly (Windows drops a
// hook whose callback is slow), e.g. by posting to the UI thread.
func (t *hookThread) deliver(events []HotkeyEvent) {
	for _, ev := range events {
		t.w.onEvent(ev)
	}
}

// StartWatcher installs the hook with the given bindings. onEvent is called
// on the hook thread and must not block.
func StartWatcher(b Bindings, onEvent func(HotkeyEvent)) (*Watcher, error) {
	w := &Watcher{onEvent: onEvent, done: make(chan struct{})}
	w.bindings.Store(&b)
	started := make(chan error, 1)
	go w.run(started)
	if err := <-started; err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Watcher) run(started chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.done)

	activeMu.Lock()
	if active != nil {
		activeMu.Unlock()
		started <- errors.New("a hotkey watcher is already running")
		return
	}
	module, _, _ := procGetModuleHandleW.Call(0)
	hook, _, err := procSetWindowsHookExW.Call(whKeyboardLL, hookCallback, module, 0)
	if hook == 0 {
		activeMu.Unlock()
		started <- fmt.Errorf("SetWindowsHookEx: %w", err)
		return
	}
	t := &hookThread{w: w, hook: hook}
	active = t
	activeMu.Unlock()
	w.threadID = windows.GetCurrentThreadId()
	timer, _, _ := procSetTimer.Call(0, 0, staleCheckMS, 0)
	started <- nil

	var msg struct {
		HWnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
		_       uint32
	}
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 { // WM_QUIT or error
			break
		}
		t.passOn() // before anything else, e.g. a timer picked before the swallow
		switch {
		case msg.Message == wmTimer && t.escTimer != 0 && msg.WParam == t.escTimer:
			procKillTimer.Call(0, t.escTimer)
			t.escTimer = 0
			t.deliver(t.filter.EscTimeout())
			t.syncMouse()
		case msg.Message == wmTimer:
			t.releaseStale()
		case msg.Message == wmSetOverlay:
			t.filter.SetOpen(msg.WParam == 1)
			t.syncMouse()
		case msg.Message == wmSyncMouse:
			t.syncMouse()
		case msg.Message == wmHandOver:
			t.passOn()
		}
	}

	t.passOn() // nothing swallowed stays behind
	procKillTimer.Call(0, timer)
	t.filter.SetOpen(false)
	t.syncMouse()
	activeMu.Lock()
	active = nil
	activeMu.Unlock()
	procUnhookWindowsHookEx.Call(hook)
}

// releaseStale treats keys that aren't down any more as released. A key
// passed on is given inFlightTime to come back through the hook first.
func (t *hookThread) releaseStale() {
	for _, vk := range t.filter.Held() {
		if at, ok := t.inFlight[vk]; ok {
			if time.Since(at) < inFlightTime {
				continue
			}
			delete(t.inFlight, vk)
		}
		if s, _, _ := procGetAsyncKeyState.Call(uintptr(vk)); s&0x8000 == 0 {
			events, _ := t.filter.Key(t.w.bindings.Load(), vk, false)
			t.deliver(events)
		}
	}
}

// SetBindings replaces the bindings. A pause key held at that moment that
// no longer is a binding reports its release with the next key event.
func (w *Watcher) SetBindings(b Bindings) { w.bindings.Store(&b) }

// LendPTT lends the hook the push-to-talk keys Resound holds, until
// TakePTT. While they are lent, a pause or stop key lets go of them before
// it reaches the game. Lend them only while a pause or stop key would let
// go of them anyway, i.e. while only playback holds them.
func (w *Watcher) LendPTT(c Combo) { w.lent.Store(&c) }

// TakePTT ends the loan. It returns the keys, or nil when a pause or stop
// key let go of them meanwhile (they are up then).
func (w *Watcher) TakePTT() Combo {
	if c := w.lent.Swap(nil); c != nil {
		return *c
	}
	return nil
}

// SetOverlayOpen tells the hook that the app opened or closed the overlay
// itself (its preview button), so Esc and the wheel are taken while it is
// open. Keys pressed meanwhile are judged by the hook's own state.
func (w *Watcher) SetOverlayOpen(open bool) {
	var wp uintptr
	if open {
		wp = 1
	}
	procPostThreadMessageW.Call(uintptr(w.threadID), wmSetOverlay, wp, 0)
}

// Close removes the hook and waits for its thread to end.
func (w *Watcher) Close() {
	w.closeMu.Do(func() {
		procPostThreadMessageW.Call(uintptr(w.threadID), wmQuit, 0, 0)
		<-w.done
	})
}
