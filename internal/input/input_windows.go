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

	mapvkVSCToVKEx = 3
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
func Press(c Combo) error { return sendRecords(pressRecords(c)) }

// Release releases the combination, in reverse order.
func Release(c Combo) error { return sendRecords(releaseRecords(c)) }

func pressRecords(c Combo) []inputRecord {
	in := make([]inputRecord, len(c))
	for i, vk := range c {
		in[i] = record(vk, true)
	}
	return in
}

func releaseRecords(c Combo) []inputRecord {
	in := make([]inputRecord, 0, len(c))
	for i := len(c) - 1; i >= 0; i-- {
		in = append(in, record(c[i], false))
	}
	return in
}

// inputRecord is one INPUT of either kind. keyboardInput and mouseInput
// have the same size, so presses of both go into one SendInput call, which
// Windows inserts without other input in between.
type inputRecord struct{ _ [5]uint64 }

// Compile-time checks that both kinds fill an inputRecord exactly.
var (
	_ = [1]struct{}{}[unsafe.Sizeof(keyboardInput{})-unsafe.Sizeof(inputRecord{})]
	_ = [1]struct{}{}[unsafe.Sizeof(mouseInput{})-unsafe.Sizeof(inputRecord{})]
)

func asRecord[T keyboardInput | mouseInput](in T) inputRecord {
	return *(*inputRecord)(unsafe.Pointer(&in))
}

func record(vk uint16, down bool) inputRecord {
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
		return asRecord(in)
	}

	in := keyInput(vk)
	if !down {
		in.Flags |= keyeventfKeyUp
	}
	return asRecord(in)
}

// keyInput describes a press of vk. Games read either the virtual key
// (window messages) or the scan code (raw input, DirectInput), so both must
// name the same key:
//   - Most keys go by scan code, which Windows turns into the virtual key
//     like a real key press.
//   - Keys whose scan code means something else go by virtual key, with the
//     scan code attached for raw input. With Num Lock off, the scan codes of
//     Numpad 0–9 and Numpad . are Insert, End, … Delete.
//   - Pause has an E1-prefixed scan code that SendInput cannot send; sent as
//     a scan code it was Right Ctrl. A real Pause arrives as VK_PAUSE with
//     scan code 0x45, so it is sent that way.
func keyInput(vk uint16) keyboardInput { return keyInputScan(vk, scanCodeEx(vk)) }

// keyInputScan is keyInput with the scan code given (with its 0xE0 or
// 0xE1 prefix), e.g. the one a real key press arrived with.
func keyInputScan(vk, sc uint16) keyboardInput {
	in := keyboardInput{Type: inputKeyboard, VK: vk, ExtraInfo: sendMarker}
	switch prefix := sc & 0xFF00; {
	case sc == 0:
	case prefix == 0xE100:
		in.Scan = 0x45
	default:
		in.Scan = sc & 0xFF
		if prefix == 0xE000 {
			in.Flags |= keyeventfExtendedKey
		}
		if back, _, _ := procMapVirtualKeyW.Call(uintptr(sc), mapvkVSCToVKEx); sameKey(uint16(back)) == sameKey(vk) {
			in.Flags |= keyeventfScanCode
		}
	}
	return in
}

// sendRecords sends all of in with one SendInput call.
func sendRecords(in []inputRecord) error {
	_, err := sendRecordsN(in)
	return err
}

// sendRecordsN sends in and returns how many Windows inserted (the first n).
func sendRecordsN(in []inputRecord) (int, error) {
	if len(in) == 0 {
		return 0, nil
	}
	n, _, err := procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
	if int(n) != len(in) {
		return int(n), fmt.Errorf("SendInput: %d of %d sent: %w", n, len(in), err)
	}
	return int(n), nil
}

func sendInput(in unsafe.Pointer, size uintptr) error {
	n, _, err := procSendInput.Call(1, uintptr(in), size)
	if n != 1 {
		return fmt.Errorf("SendInput: %w", err)
	}
	return nil
}

// scanCodeEx maps a virtual key to its scan code with its prefix: 0xE0 for
// extended keys (right Ctrl, arrows, ...), 0xE1 for Pause. 0 = none.
func scanCodeEx(vk uint16) uint16 {
	r, _, _ := procMapVirtualKeyW.Call(uintptr(vk), mapvkVKToVSCEx)
	return uint16(r)
}

// scanCode is scanCodeEx split for GetKeyNameText.
func scanCode(vk uint16) (scan uint16, extended bool) {
	sc := scanCodeEx(vk)
	return sc & 0xFF, sc&0xFF00 == 0xE000 || sc&0xFF00 == 0xE100
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
