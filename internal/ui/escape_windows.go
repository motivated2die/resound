package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Esc works while the popup is open even when another window has the
// focus: a low-level keyboard hook, installed only while the popup is
// visible, watches for it. It observes and never swallows the key, so the
// focused app still gets its Esc. When the popup has the focus itself, its
// own WM_KEYDOWN handles Esc and the hook stays out of it, so one press
// never counts twice. The hook runs on the UI thread (its message loop
// calls it); the callback only posts a message.

// wmGlobalEscape is posted to the popup when Esc goes down elsewhere.
const wmGlobalEscape = wmApp + 20

const (
	whKeyboardLL = 13
	vkEscapeLL   = 0x1B
	wmKeyUpLL    = 0x0101
	wmSysKeyUpLL = 0x0105
)

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")

	escapeCallback = windows.NewCallback(escapeHookProc) // created once: callbacks are never freed
	escapeHook     uintptr
	escapeTarget   uintptr // the popup window, while the hook is installed
	escapeDown     bool    // Esc is held: auto-repeats don't count
)

// kbdLLHookStruct is KBDLLHOOKSTRUCT.
type kbdLLHookStruct struct {
	VK, Scan, Flags, Time uint32
	Extra                 uintptr
}

func escapeHookProc(code, w uintptr, kb *kbdLLHookStruct) uintptr {
	if int32(code) == 0 /* HC_ACTION */ && escapeTarget != 0 {
		if kb.VK == vkEscapeLL {
			switch {
			case w == wmKeyUpLL || w == wmSysKeyUpLL:
				escapeDown = false
			case !escapeDown:
				escapeDown = true
				if fg, _, _ := procGetForegroundWindow.Call(); fg != escapeTarget {
					procPostMessageW.Call(escapeTarget, wmGlobalEscape, 0, 0)
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, w, uintptr(unsafe.Pointer(kb)))
	return r
}

// watchEscape installs the hook for popup window hwnd. UI thread.
func watchEscape(hwnd uintptr) {
	if escapeHook != 0 {
		return
	}
	escapeTarget, escapeDown = hwnd, false
	escapeHook, _, _ = procSetWindowsHookExW.Call(whKeyboardLL, escapeCallback, moduleHandle(), 0)
}

// unwatchEscape removes the hook. UI thread.
func unwatchEscape() {
	if escapeHook != 0 {
		procUnhookWindowsHookEx.Call(escapeHook)
	}
	escapeHook, escapeTarget = 0, 0
}
