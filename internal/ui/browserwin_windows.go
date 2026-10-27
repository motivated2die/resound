package ui

import (
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Closing the editor's browser window for its page, when the browser
// refuses the page's own window.close(). A page may only close a window
// that a script opened, or one whose history has a single entry; the edit
// page normally keeps one and closes itself, but should its history grow,
// Resound, which opened the window, closes it instead, found by a title the
// page sets just before (editbridge). Only a Chromium window whose title is exactly that
// one qualifies: an app window (Edge --app) shows the page's title alone,
// while a normal browser window adds the profile and browser name, so a
// window with other tabs never matches.

const chromeWindowClass = "Chrome_WidgetWin_1" // Edge's and Chrome's top-level windows

var (
	procEnumWindows   = user32.NewProc("EnumWindows")
	procGetClassNameW = user32.NewProc("GetClassNameW")

	enumMu      sync.Mutex // one search at a time: the callback shares these
	enumTitle   string
	enumFound   []uintptr
	enumWindows = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		var buf [256]uint16
		if n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); windows.UTF16ToString(buf[:n]) != chromeWindowClass {
			return 1
		}
		if n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); windows.UTF16ToString(buf[:n]) == enumTitle {
			enumFound = append(enumFound, hwnd)
		}
		return 1
	})
)

// browserWindows returns the visible Chromium windows titled exactly title.
func browserWindows(title string) []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumTitle, enumFound = title, nil
	procEnumWindows.Call(enumWindows, 0)
	return enumFound
}

// CloseBrowserWindow closes the browser app window titled exactly title,
// waiting up to timeout for the browser to show that title (it updates the
// window a moment after the page sets it). It reports whether it found
// one; a page in a normal browser window never matches. Any goroutine.
func CloseBrowserWindow(title string, timeout time.Duration) bool {
	for end := time.Now().Add(timeout); ; time.Sleep(50 * time.Millisecond) {
		if found := browserWindows(title); len(found) > 0 {
			for _, h := range found {
				procPostMessageW.Call(h, wmClose, 0, 0)
			}
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}
