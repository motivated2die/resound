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
// (WH_KEYBOARD_LL) on its own thread. The hook only observes: every key
// still reaches the game, which matters because a pause key is usually
// the game's push-to-talk for another channel. RegisterHotKey would
// swallow the key instead.
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
	wmQuit       = 0x0012
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

// The hook callback is a plain function; it finds its watcher here. Only
// one Watcher runs at a time.
var (
	activeMu sync.Mutex
	active   *hookThread
)

type hookThread struct {
	w       *Watcher
	hook    uintptr
	matcher Matcher
}

var hookCallback = windows.NewCallback(func(code, wparam uintptr, k *kbdLLHookStruct) uintptr {
	if int32(code) >= 0 {
		activeMu.Lock()
		t := active
		activeMu.Unlock()
		if t != nil {
			if k.ExtraInfo != sendMarker && k.VKCode > 0 && k.VKCode < 0xFF {
				down := (wparam == wmKeyDown || wparam == wmSysKeyDown) && k.Flags&llkhfUp == 0
				t.deliver(t.matcher.Key(t.w.bindings.Load(), uint16(k.VKCode), down))
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wparam, uintptr(unsafe.Pointer(k)))
	return r
})

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
		if msg.Message == wmTimer {
			t.releaseStale()
		}
	}

	procKillTimer.Call(0, timer)
	activeMu.Lock()
	active = nil
	activeMu.Unlock()
	procUnhookWindowsHookEx.Call(hook)
}

// releaseStale treats keys that aren't down any more as released.
func (t *hookThread) releaseStale() {
	for _, vk := range t.matcher.Held() {
		if s, _, _ := procGetAsyncKeyState.Call(uintptr(vk)); s&0x8000 == 0 {
			t.deliver(t.matcher.Key(t.w.bindings.Load(), vk, false))
		}
	}
}

// SetBindings replaces the bindings. A pause key held at that moment that
// no longer is a binding reports its release with the next key event.
func (w *Watcher) SetBindings(b Bindings) { w.bindings.Store(&b) }

// Close removes the hook and waits for its thread to end.
func (w *Watcher) Close() {
	w.closeMu.Do(func() {
		procPostThreadMessageW.Call(uintptr(w.threadID), wmQuit, 0, 0)
		<-w.done
	})
}
