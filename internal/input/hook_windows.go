package input

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
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
// Resound's own push-to-talk presses carry a marker (sendMarker) and are
// ignored, so they can never trigger a hotkey. Like SendInput, the hook does
// not see keys typed into a program running as administrator.
type Watcher struct {
	bindings atomic.Pointer[Bindings]
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
	wmSyncMouse  = wmApp + 1 // install or remove the mouse hook to match the overlay
	wmSetOverlay = wmApp + 2 // wParam 1: the app opened the overlay, 0: closed it
	wmTimer      = 0x0113
	wmKeyDown    = 0x0100
	wmSysKeyDown = 0x0104
	llkhfUp      = 0x80

	// staleCheck is how often keys believed held are checked against the
	// real keyboard state, in case a release never reached the hook (the
	// secure desktop of Ctrl+Alt+Del or UAC, a lock screen, an elevated
	// window taking focus).
	staleCheckMS = 250
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
	filter    Filter
}

func activeThread() *hookThread {
	activeMu.Lock()
	defer activeMu.Unlock()
	return active
}

var hookCallback = windows.NewCallback(func(code, wparam uintptr, k *kbdLLHookStruct) uintptr {
	if int32(code) >= 0 {
		if t := activeThread(); t != nil && k.ExtraInfo != sendMarker && k.VKCode > 0 && k.VKCode < 0xFF {
			down := (wparam == wmKeyDown || wparam == wmSysKeyDown) && k.Flags&llkhfUp == 0
			wasOpen := t.filter.Open()
			events, swallow := t.filter.Key(t.w.bindings.Load(), uint16(k.VKCode), down)
			t.deliver(events)
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
		switch msg.Message {
		case wmTimer:
			t.releaseStale()
		case wmSetOverlay:
			t.filter.SetOpen(msg.WParam == 1)
			t.syncMouse()
		case wmSyncMouse:
			t.syncMouse()
		}
	}

	procKillTimer.Call(0, timer)
	t.filter.SetOpen(false)
	t.syncMouse()
	activeMu.Lock()
	active = nil
	activeMu.Unlock()
	procUnhookWindowsHookEx.Call(hook)
}

// releaseStale treats keys that aren't down any more as released.
func (t *hookThread) releaseStale() {
	for _, vk := range t.filter.Held() {
		if s, _, _ := procGetAsyncKeyState.Call(uintptr(vk)); s&0x8000 == 0 {
			events, _ := t.filter.Key(t.w.bindings.Load(), vk, false)
			t.deliver(events)
		}
	}
}

// SetBindings replaces the bindings. A pause key held at that moment that
// no longer is a binding reports its release with the next key event.
func (w *Watcher) SetBindings(b Bindings) { w.bindings.Store(&b) }

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
