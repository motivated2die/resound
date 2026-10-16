package ui

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
)

const gmemMoveable = 0x0002

// SetClipboardText puts s on the clipboard as text, owned by window hwnd.
// Another program may hold the clipboard open for a moment, so opening it
// is tried a few times.
func SetClipboardText(hwnd uintptr, s string) error {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return err
	}
	opened := false
	for range 10 {
		if r, _, _ := procOpenClipboard.Call(hwnd); r != 0 {
			opened = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !opened {
		return fmt.Errorf("the clipboard is in use by another program")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()

	size := uintptr(len(u)) * 2
	mem, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if mem == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	p, _, err := procGlobalLock.Call(mem)
	if p == 0 {
		procGlobalFree.Call(mem)
		return fmt.Errorf("GlobalLock: %w", err)
	}
	copy(unsafe.Slice((*uint16)(unsafe.Add(nil, p)), len(u)), u)
	procGlobalUnlock.Call(mem)
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, mem); r == 0 {
		procGlobalFree.Call(mem) // still ours when SetClipboardData fails
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	return nil
}
