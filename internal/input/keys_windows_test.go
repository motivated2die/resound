package input

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// presetScan is each preset key's scan code as a real keyboard reports it in
// window messages and raw input (scan code set 1; 0xE0 = extended). Pause
// is reported as 0x45 in window messages.
var presetScan = map[uint16]uint16{
	0x13: 0x45, 0x91: 0x46,
	VKDivide: 0xE035, VKMultiply: 0x37, VKSubtract: 0x4A, VKAdd: 0x4E,
	VKNumpad0: 0x52, VKNumpad0 + 1: 0x4F, VKNumpad0 + 2: 0x50, VKNumpad0 + 3: 0x51, VKNumpad0 + 4: 0x4B,
	VKNumpad0 + 5: 0x4C, VKNumpad0 + 6: 0x4D, VKNumpad0 + 7: 0x47, VKNumpad0 + 8: 0x48, VKNumpad0 + 9: 0x49,
	0x7C: 0x64, 0x7D: 0x65, 0x7E: 0x66, 0x7F: 0x67, 0x80: 0x68, 0x81: 0x69,
	0x82: 0x6A, 0x83: 0x6B, 0x84: 0x6C, 0x85: 0x6D, 0x86: 0x6E, 0x87: 0x76,
}

// presetKeys are the keyboard keys of the presets.
func presetKeys(t *testing.T) []uint16 {
	var keys []uint16
	for _, p := range Presets {
		if len(p.Keys) != 1 {
			t.Fatalf("preset %v is not a single key", p.Keys)
		}
		if !IsMouse(p.Keys[0]) {
			keys = append(keys, p.Keys[0])
		}
	}
	return keys
}

// TestPresetKeyInput checks what Resound would send for each preset without
// sending it: the scan code is the key's own, and when Windows is left to
// turn the scan code into a virtual key, it turns into the same key.
func TestPresetKeyInput(t *testing.T) {
	for _, vk := range presetKeys(t) {
		want, ok := presetScan[vk]
		if !ok {
			t.Errorf("%s (%#x): no expected scan code", Name(vk), vk)
			continue
		}
		in := keyInput(vk)
		got := in.Scan
		if in.Flags&keyeventfExtendedKey != 0 {
			got |= 0xE000
		}
		if in.VK != vk || got != want {
			t.Errorf("%s: sends vk %#x scan %#x, want vk %#x scan %#x", Name(vk), in.VK, got, vk, want)
		}
		if in.Flags&keyeventfScanCode != 0 {
			if back, _, _ := procMapVirtualKeyW.Call(uintptr(got), mapvkVSCToVKEx); uint16(back) != vk {
				t.Errorf("%s: scan code %#x is %#x to Windows", Name(vk), got, back)
			}
		}
	}
	// Numpad digits must not depend on Num Lock, so they go by virtual key.
	if keyInput(VKNumpad0).Flags&keyeventfScanCode != 0 {
		t.Error("Numpad 0 is sent by scan code: with Num Lock off it would be Insert")
	}
}

// TestKeyInputModifiers keeps keys recorded in the panel as they were: left
// and right modifiers, and generic Ctrl, go by scan code.
func TestKeyInputModifiers(t *testing.T) {
	for _, vk := range []uint16{0x11, 0xA2, 0xA3, 0xA0, 0xA4, 0x41, 0x26} {
		if in := keyInput(vk); in.Flags&keyeventfScanCode == 0 || in.Scan == 0 {
			t.Errorf("%s (%#x): flags %#x scan %#x, want a scan code", Name(vk), vk, in.Flags, in.Scan)
		}
	}
}

// TestPresetsLive presses every preset key into a window of its own and
// checks what the window sees in WM_KEYDOWN and raw input, with Num Lock
// on and off. Keys remapped on the PC (PowerToys Keyboard Manager, mouse and
// keyboard software) fail, since those tools rewrite injected keys too. It
// presses real keys, so it only runs on request:
//
//	$env:RESOUND_KEYS_TEST="1"; go test -run PresetsLive -v ./internal/input
func TestPresetsLive(t *testing.T) {
	if os.Getenv("RESOUND_KEYS_TEST") != "1" {
		t.Skip("set RESOUND_KEYS_TEST=1 to press the preset keys")
	}
	p := startProbe(t)
	defer p.stop()

	numLockOn := func() bool { s, _, _ := procGetKeyState.Call(0x90); return s&1 != 0 }
	toggleNumLock := func() {
		for _, f := range []uint32{keyeventfExtendedKey, keyeventfExtendedKey | keyeventfKeyUp} {
			in := keyboardInput{Type: inputKeyboard, VK: 0x90, Scan: 0x45, Flags: f}
			_ = sendInput(unsafe.Pointer(&in), unsafe.Sizeof(in))
		}
		time.Sleep(100 * time.Millisecond)
		p.take()
	}
	initial := numLockOn()
	defer func() {
		if numLockOn() != initial {
			toggleNumLock()
		}
	}()

	for range 2 {
		t.Run(fmt.Sprintf("numlock=%v", numLockOn()), func(t *testing.T) {
			for _, vk := range presetKeys(t) {
				checkLive(t, p, vk)
			}
		})
		toggleNumLock()
	}
}

func checkLive(t *testing.T, p *probe, vk uint16) {
	t.Helper()
	want := presetScan[vk]
	if err := Press(Combo{vk}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	var stray []string
	for _, mod := range []uint16{0x10, 0x11, 0x12, 0x5B} { // Shift, Ctrl, Alt, Win
		if s, _, _ := procGetAsyncKeyState.Call(uintptr(mod)); s&0x8000 != 0 {
			stray = append(stray, Name(mod))
		}
	}
	held, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	if err := Release(Combo{vk}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if len(stray) > 0 {
		t.Errorf("%s: %v held down with it", Name(vk), stray)
	}
	if held&0x8000 == 0 {
		t.Errorf("%s: not reported down while held", Name(vk))
	}
	seen := map[string]bool{}
	for _, e := range p.take() {
		seen[e.src] = true
		if e.vk != uint32(vk) || e.scan != uint32(want) {
			t.Errorf("%s: %s saw vk %#x scan %#x, want vk %#x scan %#x", Name(vk), e.src, e.vk, e.scan, vk, want)
		}
	}
	for _, src := range []string{"WM", "RAW"} {
		if !seen[src] && (src == "RAW" || p.foreground) {
			t.Errorf("%s: nothing seen in %s", Name(vk), src)
		}
	}
}

// --- a window that records the keys it gets ---------------------------------

type probeEvent struct {
	src      string // "WM" or "RAW"
	vk, scan uint32 // scan with 0xE000 for extended keys
}

type probe struct {
	foreground bool
	tid        uint32
	done       chan struct{}
	mu         sync.Mutex
	events     []probeEvent
}

var (
	procRegisterClassExW        = user32.NewProc("RegisterClassExW")
	procCreateWindowExW         = user32.NewProc("CreateWindowExW")
	procDefWindowProcW          = user32.NewProc("DefWindowProcW")
	procShowWindow              = user32.NewProc("ShowWindow")
	procSetForegroundWindow     = user32.NewProc("SetForegroundWindow")
	procRegisterRawInputDevices = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData         = user32.NewProc("GetRawInputData")
	procDispatchMessageW        = user32.NewProc("DispatchMessageW")
	procDestroyWindow           = user32.NewProc("DestroyWindow")
	procGetKeyState             = user32.NewProc("GetKeyState")

	activeProbe *probe // the window procedure's probe
	probeProc   = windows.NewCallback(probeWndProc)
)

func (p *probe) add(e probeEvent) { p.mu.Lock(); p.events = append(p.events, e); p.mu.Unlock() }

func (p *probe) take() []probeEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	ev := p.events
	p.events = nil
	return ev
}

func (p *probe) stop() {
	procPostThreadMessageW.Call(uintptr(p.tid), wmQuit, 0, 0)
	<-p.done
}

func probeWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	p := activeProbe
	switch msg {
	case 0x0100, 0x0104: // WM_KEYDOWN, WM_SYSKEYDOWN
		scan := uint32(lp>>16) & 0xFF
		if lp&(1<<24) != 0 {
			scan |= 0xE000
		}
		p.add(probeEvent{"WM", uint32(wp), scan})
		return 0
	case 0x00FF: // WM_INPUT
		var ri struct {
			Type, Size                      uint32
			Device, WParam                  uintptr
			MakeCode, Flags, Reserved, VKey uint16
			Message, Extra                  uint32
		}
		size := uint32(unsafe.Sizeof(ri))
		procGetRawInputData.Call(lp, 0x10000003 /* RID_INPUT */, uintptr(unsafe.Pointer(&ri)), uintptr(unsafe.Pointer(&size)), 24)
		if ri.Type == 1 && ri.Flags&1 == 0 { // keyboard, key down
			scan := uint32(ri.MakeCode)
			switch {
			case ri.Flags&2 != 0: // RI_KEY_E0
				scan |= 0xE000
			case ri.Flags&4 != 0: // RI_KEY_E1
				scan |= 0xE100
			}
			p.add(probeEvent{"RAW", uint32(ri.VKey), scan})
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// startProbe opens the window on a thread of its own, takes the focus and
// registers for raw keyboard input even without it.
func startProbe(t *testing.T) *probe {
	p := &probe{done: make(chan struct{})}
	activeProbe = p
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer close(p.done)
		p.tid = windows.GetCurrentThreadId()
		mod, _, _ := procGetModuleHandleW.Call(0)
		cls, _ := windows.UTF16PtrFromString("ResoundKeyTest")
		wc := struct {
			Size, Style                        uint32
			WndProc                            uintptr
			ClsExtra, WndExtra                 int32
			Instance, Icon, Cursor, Background uintptr
			MenuName, ClassName                *uint16
			IconSm                             uintptr
		}{WndProc: probeProc, Instance: mod, ClassName: cls}
		wc.Size = uint32(unsafe.Sizeof(wc))
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(cls)),
			0x10CF0000 /* WS_OVERLAPPEDWINDOW | WS_VISIBLE */, 100, 100, 300, 200, 0, 0, mod, 0)
		procShowWindow.Call(hwnd, 5)
		procSetForegroundWindow.Call(hwnd)
		rid := struct {
			Page, Usage uint16
			Flags       uint32
			Target      uintptr
		}{1, 6, 0x100 /* RIDEV_INPUTSINK */, hwnd}
		procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rid)), 1, unsafe.Sizeof(rid))
		time.Sleep(200 * time.Millisecond)
		fg, _, _ := procGetForegroundWindow.Call()
		p.foreground = fg == hwnd
		close(ready)
		var msg [48]byte
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(r) <= 0 {
				break
			}
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
		procDestroyWindow.Call(hwnd)
	}()
	<-ready
	if !p.foreground {
		p.stop()
		t.Skip("the test window didn't get the focus; the keys would go to another program")
	}
	return p
}
