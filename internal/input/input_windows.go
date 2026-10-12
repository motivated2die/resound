// Package input presses and releases push-to-talk keys and mouse buttons
// with SendInput. Keys are sent as hardware scan codes, which games that read
// raw input or DirectInput see like a real key press.
//
// Windows blocks input from a normal process into an elevated one (UIPI):
// if a game runs as administrator, it will not see these presses.
package input

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	procSendInput       = user32.NewProc("SendInput")
	procMapVirtualKeyW  = user32.NewProc("MapVirtualKeyW")
	procGetKeyNameTextW = user32.NewProc("GetKeyNameTextW")
)

// Virtual-key codes of the mouse buttons a push-to-talk can use.
const (
	VKMiddle   = 0x04
	VKXButton1 = 0x05 // "Mouse 4" (back)
	VKXButton2 = 0x06 // "Mouse 5" (forward)
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	keyeventfExtendedKey = 0x0001
	keyeventfKeyUp       = 0x0002
	keyeventfScanCode    = 0x0008

	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040
	mouseeventfXDown      = 0x0080
	mouseeventfXUp        = 0x0100

	mapvkVKToVSCEx = 4

	// sendMarker tags Resound's own presses (dwExtraInfo), so the hotkey
	// hook can tell them from the user's ("RSND").
	sendMarker = 0x52534E44
)

// keyboardInput and mouseInput mirror INPUT with KEYBDINPUT / MOUSEINPUT
// (both 40 bytes on x64; the union is sized by MOUSEINPUT).
type keyboardInput struct {
	Type      uint32
	_         uint32
	VK        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
	_         [8]byte
}

type mouseInput struct {
	Type      uint32
	_         uint32
	DX, DY    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

// Combo is a key combination, in the order the keys are pressed.
type Combo []uint16

// String names the combination, e.g. "Ctrl + Mouse 4".
func (c Combo) String() string {
	names := make([]string, len(c))
	for i, vk := range c {
		names[i] = Name(vk)
	}
	return strings.Join(names, " + ")
}

// Press presses every key of the combination, in order.
func Press(c Combo) error {
	for _, vk := range c {
		if err := send(vk, true); err != nil {
			return err
		}
	}
	return nil
}

// Release releases the combination, in reverse order.
func Release(c Combo) error {
	var firstErr error
	for i := len(c) - 1; i >= 0; i-- {
		if err := send(c[i], false); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func send(vk uint16, down bool) error {
	switch vk {
	case VKMiddle, VKXButton1, VKXButton2:
		in := mouseInput{Type: inputMouse, ExtraInfo: sendMarker}
		switch {
		case vk == VKMiddle && down:
			in.Flags = mouseeventfMiddleDown
		case vk == VKMiddle:
			in.Flags = mouseeventfMiddleUp
		default:
			in.MouseData = uint32(vk - VKXButton1 + 1) // XBUTTON1 = 1, XBUTTON2 = 2
			in.Flags = mouseeventfXUp
			if down {
				in.Flags = mouseeventfXDown
			}
		}
		return sendInput(unsafe.Pointer(&in), unsafe.Sizeof(in))
	}

	in := keyboardInput{Type: inputKeyboard, VK: vk, ExtraInfo: sendMarker}
	scan, extended := scanCode(vk)
	if scan != 0 {
		in.Scan = scan
		in.Flags |= keyeventfScanCode
		if extended {
			in.Flags |= keyeventfExtendedKey
		}
	}
	if !down {
		in.Flags |= keyeventfKeyUp
	}
	return sendInput(unsafe.Pointer(&in), unsafe.Sizeof(in))
}

func sendInput(in unsafe.Pointer, size uintptr) error {
	n, _, err := procSendInput.Call(1, uintptr(in), size)
	if n != 1 {
		return fmt.Errorf("SendInput: %w", err)
	}
	return nil
}

// scanCode maps a virtual key to its scan code; extended keys (right Ctrl,
// arrows, ...) carry an 0xE0 prefix.
func scanCode(vk uint16) (scan uint16, extended bool) {
	r, _, _ := procMapVirtualKeyW.Call(uintptr(vk), mapvkVKToVSCEx)
	return uint16(r & 0xFF), r&0xFF00 == 0xE000 || r&0xFF00 == 0xE100
}

// Name returns a readable key name in the user's keyboard language.
func Name(vk uint16) string {
	if name, ok := fixedName(vk); ok {
		return name
	}
	scan, extended := scanCode(vk)
	if scan == 0 {
		return fmt.Sprintf("Key 0x%02X", vk)
	}
	lparam := uintptr(scan) << 16
	if extended {
		lparam |= 1 << 24
	}
	buf := make([]uint16, 64)
	n, _, _ := procGetKeyNameTextW.Call(lparam, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return fmt.Sprintf("Key 0x%02X", vk)
	}
	name := windows.UTF16ToString(buf[:n])
	switch vk { // GetKeyNameText doesn't say which side these are on
	case 0xA0, 0xA2, 0xA4:
		return "Left " + name
	case 0xA1, 0xA3, 0xA5:
		if !strings.HasPrefix(strings.ToLower(name), "right") {
			return "Right " + name
		}
	}
	return name
}
