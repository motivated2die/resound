package ui

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// layered is a window that shows over games without ever taking the focus:
// the in-game overlay (overlay_windows.go) and the launchpad's badge
// (badge_windows.go). It is a layered window (per-pixel alpha, drawn with
// UpdateLayeredWindow), topmost, a tool window (no taskbar button), never
// activated, and transparent to the mouse, so clicks reach the game. Its
// owner draws the frames (setFrame); layered slides it in and out, moving
// and fading it, places it with the owner's place function, which clips it
// to the screen, and calls the owner's tick on a timer while the owner
// animates something.
type layered struct {
	hwnd       uintptr
	surf       *surface
	mon        rect
	phase      overlayPhase
	phaseStart time.Time
	timer      bool
	onHidden   func()
	// place puts a w×h frame on mon, slid in by p (0..1): where its shown
	// part goes on the screen, where that starts in the frame, its size.
	place func(mon rect, w, h int32, p float64) (dst, src point, cw, ch int32)
	tick  func()
}

type overlayPhase int

const (
	ovHidden overlayPhase = iota
	ovShowing
	ovShown
	ovHiding
)

var (
	layeredWindows = map[uintptr]*layered{} // for the window procedure
	layeredClass   bool                     // registered
)

// newLayered creates a hidden layered window; tick runs on its timer.
func newLayered(tick func()) (*layered, error) {
	if _, err := getD2D(); err != nil {
		return nil, fmt.Errorf("drawing the overlay: %w", err)
	}
	if !layeredClass {
		if err := registerClass(overlayClass, 0, windows.NewCallback(layeredProc)); err != nil {
			return nil, err
		}
		layeredClass = true
	}
	h, err := createWindow(wsExLayered|wsExTransparent|wsExToolWindow|wsExTopmost|wsExNoActivate, wsPopup, overlayClass)
	if err != nil {
		return nil, err
	}
	l := &layered{hwnd: h, tick: tick}
	layeredWindows[h] = l
	return l, nil
}

func layeredProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmTimer:
		if lw := layeredWindows[hwnd]; lw != nil && lw.tick != nil {
			lw.tick()
		}
		return 0
	case wmMouseActivate:
		return maNoActivate
	}
	return defWindowProc(hwnd, m, w, l)
}

// Visible reports whether the window is showing (also while it slides in).
func (l *layered) Visible() bool { return l.phase != ovHidden && l.phase != ovHiding }

// open starts sliding in on the primary screen, unless it is in already.
func (l *layered) open(now time.Time) (scale float64) {
	mon, scale := primaryMonitor()
	l.mon = mon
	if l.phase == ovHidden || l.phase == ovHiding {
		l.phase, l.phaseStart = ovShowing, now
	}
	l.onHidden = nil // opened again during a slide-out
	return scale
}

// raise puts it on top of the topmost windows again: a game may have put
// itself above since.
func (l *layered) raise() {
	procSetWindowPos.Call(l.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpShowWindow)
}

// Hide slides it out; onHidden (if set) runs once it is gone.
func (l *layered) Hide(onHidden func()) {
	if l.phase == ovHidden {
		if onHidden != nil {
			onHidden()
		}
		return
	}
	if l.phase != ovHiding {
		l.phase, l.phaseStart = ovHiding, time.Now()
	}
	l.onHidden = onHidden
	l.startTimer()
}

func (l *layered) startTimer() {
	if !l.timer {
		procSetTimer.Call(l.hwnd, overlayTimerID, overlayFrameMS, 0)
		l.timer = true
	}
}

func (l *layered) stopTimer() {
	if l.timer {
		procKillTimer.Call(l.hwnd, overlayTimerID)
		l.timer = false
	}
}

// slide returns the slide-in progress (0 hidden at the edge, 1 in place).
func (l *layered) slide(now time.Time) float64 {
	el := now.Sub(l.phaseStart)
	switch l.phase {
	case ovShowing:
		return easeOut(float64(el) / float64(overlaySlideIn))
	case ovHiding:
		return 1 - easeOut(float64(el)/float64(overlaySlideOut))
	case ovShown:
		return 1
	}
	return 0
}

// advance moves the slide on: shown once in, hidden once out. gone reports
// that it just went; the owner then resets what it shows and calls gone.
func (l *layered) advance(now time.Time) (p float64, gone bool) {
	p = l.slide(now)
	switch {
	case l.phase == ovShowing && p >= 1:
		l.phase = ovShown
	case l.phase == ovHiding && p <= 0:
		l.phase = ovHidden
		l.stopTimer()
		procShowWindow.Call(l.hwnd, swHide)
		return 0, true
	}
	return p, false
}

// gone runs onHidden after the window went.
func (l *layered) gone() {
	if f := l.onHidden; f != nil {
		l.onHidden = nil
		f()
	}
}

// setFrame replaces the frame shown; a nil one (drawing failed) keeps the
// last.
func (l *layered) setFrame(surf *surface) {
	if surf == nil {
		return
	}
	l.surf.free()
	l.surf = surf
}

// present puts the current frame on screen, slid in by p (0..1) and as
// opaque as far as it slid.
func (l *layered) present(p float64) {
	if l.surf == nil {
		return
	}
	dst, src, w, h := l.place(l.mon, l.surf.w, l.surf.h, p)
	if w <= 0 || h <= 0 {
		return
	}
	screen, _, _ := procGetDC.Call(0)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	old, _, _ := procSelectObject.Call(mem, l.surf.bmp)
	size := struct{ CX, CY int32 }{w, h}
	alpha := uint32(max(0, min(255, p*255)))
	blend := uint32(0 | 0<<8 | alpha<<16 | 1<<24) // AC_SRC_OVER, constant alpha, AC_SRC_ALPHA
	procUpdateLayeredWindow.Call(l.hwnd, screen, uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&size)),
		mem, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	procSelectObject.Call(mem, old)
	procDeleteDC.Call(mem)
	procReleaseDC.Call(0, screen)
	procShowWindow.Call(l.hwnd, swShowNoActivate)
}
