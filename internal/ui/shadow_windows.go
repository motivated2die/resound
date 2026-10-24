package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// shadow is a click-through layered window right below the popup in the
// z-order, showing a blurred translucent black copy of its shape. It never
// takes focus, and clicks on it go to the window under it.
type shadow struct {
	hwnd uintptr
	// Last rendered size, to re-render only when the popup's size changes.
	w, h  int32
	scale float64
	dib   *dib
	pad   int32
}

const shadowClass = "ResoundShadow"

const (
	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	ulwAlpha        = 2
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
)

var procUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")

var shadowRegistered bool // UI thread only: the panel and the tray menu each have a shadow

func newShadow() (*shadow, error) {
	if !shadowRegistered {
		if err := registerClass(shadowClass, 0, windows.NewCallback(defWindowProc)); err != nil {
			return nil, err
		}
		shadowRegistered = true
	}
	h, err := createWindow(wsExLayered|wsExTransparent|wsExToolWindow|wsExTopmost|wsExNoActivate, wsPopup, shadowClass)
	if err != nil {
		return nil, err
	}
	return &shadow{hwnd: h}, nil
}

// place shows the shadow around popup rectangle r (screen pixels), directly
// beneath window above (the popup, or the panel's tab).
func (s *shadow) place(above uintptr, r rect, scale float64) {
	if s == nil {
		return
	}
	if s.dib == nil || r.w() != s.w || r.h() != s.h || scale != s.scale {
		s.render(r.w(), r.h(), scale)
	}
	if s.dib == nil {
		return
	}
	screen, _, _ := procGetDC.Call(0)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	old, _, _ := procSelectObject.Call(mem, s.dib.bmp)
	dst := s.origin(r)
	size := struct{ CX, CY int32 }{s.dib.w, s.dib.h}
	var src point
	blend := uint32(0 | 0<<8 | 255<<16 | 1<<24) // AC_SRC_OVER, alpha 255, AC_SRC_ALPHA
	procUpdateLayeredWindow.Call(s.hwnd, screen, uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&size)),
		mem, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	procSelectObject.Call(mem, old)
	procDeleteDC.Call(mem)
	procReleaseDC.Call(0, screen)
	// Right below it (it is topmost); no activation.
	procSetWindowPos.Call(s.hwnd, above, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpShowWindow)
}

// origin is the shadow window's top left corner around popup rectangle r.
func (s *shadow) origin(r rect) point { return point{r.Left - s.pad, r.Top - s.pad} }

func (s *shadow) hide() {
	if s != nil {
		procShowWindow.Call(s.hwnd, swHide)
	}
}

// render builds the shadow bitmap for a popup of w×h device pixels.
func (s *shadow) render(w, h int32, scale float64) {
	px := func(v int) int { return int(float64(v)*scale + 0.5) }
	blur, off := px(shadowSpec.blur), px(shadowSpec.offsetY)
	pad := blur + off
	mask, cw, ch := shadowMask(int(w), int(h), pad, blur, off, px(shadowSpec.corner), shadowSpec.opacity)
	pix := make([]uint8, 4*len(mask)) // premultiplied black: only alpha is set
	for i, a := range mask {
		pix[4*i+3] = a
	}
	if s.dib != nil {
		procDeleteObject.Call(s.dib.bmp)
	}
	s.dib = newDIB(pix, int32(cw), int32(ch))
	s.w, s.h, s.scale, s.pad = w, h, scale, int32(pad)
}
