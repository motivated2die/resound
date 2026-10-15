//go:build windows

package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

var procPeekMessageW = user32.NewProc("PeekMessageW")

// pump runs the thread's messages (the overlay's animation timer) for d.
func pump(d time.Duration) {
	var m msg
	for end := time.Now().Add(d); time.Now().Before(end); {
		for {
			if r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 1 /* PM_REMOVE */); r == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestOverlayLive shows the real overlay on the primary screen for a
// moment: it must not take the focus from the window that has it, must
// show where expected, and must be gone after hiding. With
// RESOUND_RENDER_DIR set it saves a screenshot of the screen's edge.
//
//	$env:RESOUND_OVERLAY_LIVE="1"; go test -run OverlayLive -v ./internal/ui
func TestOverlayLive(t *testing.T) {
	if os.Getenv("RESOUND_OVERLAY_LIVE") != "1" {
		t.Skip("set RESOUND_OVERLAY_LIVE=1 to show the overlay on the screen")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	InitDPIAwareness()
	o, err := NewOverlay()
	if err != nil {
		t.Fatal(err)
	}
	fg, _, _ := procGetForegroundWindow.Call()
	mon, _ := primaryMonitor()
	o.Show(overlayTestItems, 3, false, "")
	// While it slides in, it never reaches past the primary screen.
	for end := time.Now().Add(250 * time.Millisecond); time.Now().Before(end); {
		pump(5 * time.Millisecond)
		var r rect
		procGetWindowRect.Call(o.hwnd, uintptr(unsafe.Pointer(&r)))
		if r.Right > mon.Right || r.Left < mon.Left {
			t.Fatalf("while sliding in: window %+v past the screen %+v", r, mon)
		}
	}
	o.HoldToStop(700 * time.Millisecond)
	pump(400 * time.Millisecond) // the bar turning red
	o.SetSelected(4)
	pump(250 * time.Millisecond)
	if now, _, _ := procGetForegroundWindow.Call(); now != fg {
		t.Errorf("the overlay took the focus: foreground %#x, was %#x", now, fg)
	}
	var r rect
	procGetWindowRect.Call(o.hwnd, uintptr(unsafe.Pointer(&r)))
	if r.Right != mon.Right || r.Left < mon.Left {
		t.Errorf("window %+v not at the right edge of %+v", r, mon)
	}
	if dir := os.Getenv("RESOUND_RENDER_DIR"); dir != "" {
		saveScreen(t, r, filepath.Join(dir, "overlay-live.png"))
	}
	o.Hide(nil)
	pump(250 * time.Millisecond)
	if visible, _, _ := procIsWindowVisible.Call(o.hwnd); visible != 0 || o.Visible() {
		t.Error("still visible after hiding")
	}
}

// saveScreen saves what the screen shows in r, layered windows included.
func saveScreen(t *testing.T, r rect, path string) {
	s, err := newSurface(r.w(), r.h())
	if err != nil {
		t.Fatal(err)
	}
	defer s.free()
	screen, _, _ := procGetDC.Call(0)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	old, _, _ := procSelectObject.Call(mem, s.bmp)
	procBitBlt.Call(mem, 0, 0, uintptr(r.w()), uintptr(r.h()), screen, uintptr(r.Left), uintptr(r.Top), 0x00CC0020|0x40000000 /* SRCCOPY | CAPTUREBLT */)
	procGdiFlush.Call()
	img := image.NewNRGBA(image.Rect(0, 0, int(r.w()), int(r.h())))
	for i := 0; i < int(r.w()*r.h()); i++ {
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = s.bits[4*i+2], s.bits[4*i+1], s.bits[4*i], 255
	}
	procSelectObject.Call(mem, old)
	procDeleteDC.Call(mem)
	procReleaseDC.Call(0, screen)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_ = png.Encode(f, img)
}
