package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// closeTab is the "Close" tab that peeks out above the popup's top right
// corner. The popup stays open when another window gets the focus, so
// closing it is a deliberate click here (or Esc, or the tray icon). The tab
// is a window of its own, right below the popup in the z-order: the popup
// covers its lower edge, so it looks tucked under the panel.
type closeTab struct {
	hwnd   uintptr
	popup  *Popup
	w, h   int32 // current size, device pixels
	hover  bool
	press  bool
	tracks bool // TrackMouseEvent is armed
}

const (
	closeTabClass  = "ResoundCloseTab"
	closeTabWL     = 84 // logical size
	closeTabHL     = 36
	closeTabUnderL = 12 // the part hidden under the popup
	closeTabInsetL = 14 // from the popup's right edge
)

var procSetWindowRgn = user32.NewProc("SetWindowRgn")

func newCloseTab(p *Popup) (*closeTab, error) {
	t := &closeTab{popup: p}
	if err := registerClass(closeTabClass, 0, windows.NewCallback(t.wndProc)); err != nil {
		return nil, err
	}
	h, err := createWindow(wsExToolWindow|wsExTopmost|wsExNoActivate, wsPopup, closeTabClass)
	if err != nil {
		return nil, err
	}
	t.hwnd = h
	return t, nil
}

// visibleH is the height of the part above the popup, device pixels.
func (t *closeTab) visibleH(c *canvas) int32 { return c.px(closeTabHL - closeTabUnderL) }

// place shows the tab above popup rectangle r (screen pixels), right below
// the popup in the z-order. Call it after placing the shadow, so the tab
// sits between the two.
func (t *closeTab) place(r rect) {
	c := t.popup.canvas(0)
	w, h := c.px(closeTabWL), c.px(closeTabHL)
	if w != t.w || h != t.h {
		t.w, t.h = w, h
		radius := c.px(20)
		rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), uintptr(radius), uintptr(radius))
		procSetWindowRgn.Call(t.hwnd, rgn, 1) // the window owns the region now
	}
	x := r.Right - c.px(closeTabInsetL) - w
	y := r.Top - t.visibleH(c)
	procSetWindowPos.Call(t.hwnd, t.popup.hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoActivate|swpShowWindow)
	procInvalidateRect.Call(t.hwnd, 0, 0)
}

func (t *closeTab) hide() {
	t.hover, t.press = false, false
	procShowWindow.Call(t.hwnd, swHide)
}

const (
	wmMouseActivate = 0x0021
	maNoActivate    = 3
)

func (t *closeTab) wndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmMouseActivate:
		return maNoActivate // clicking the tab doesn't take the focus
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		t.paint()
		return 0
	case wmMouseMove:
		if !t.tracks {
			tme := trackMouseEventStruct{Flags: tmeLeave, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			t.tracks = true
		}
		if !t.hover {
			t.hover = true
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmMouseLeave:
		t.tracks, t.hover, t.press = false, false, false
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonDown:
		t.press = true
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonUp:
		if t.press && t.hover {
			t.press = false
			t.popup.Hide()
			return 0
		}
		t.press = false
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

func (t *closeTab) paint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(t.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(t.hwnd, uintptr(unsafe.Pointer(&ps)))
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(t.w), uintptr(t.h))
	old, _, _ := procSelectObject.Call(mem, bmp)
	defer func() {
		procSelectObject.Call(mem, old)
		procDeleteObject.Call(bmp)
		procDeleteDC.Call(mem)
	}()

	t.draw(t.popup.canvas(mem))
	procBitBlt.Call(hdc, 0, 0, uintptr(t.w), uintptr(t.h), mem, 0, 0, 0x00CC0020 /* SRCCOPY */)
}

// draw paints the tab: its background, then the X icon and "Close", centred
// in the part above the popup.
func (t *closeTab) draw(c *canvas) {
	bg, fg := colBackground, colMuted
	switch {
	case t.press:
		bg, fg = colRowPress, colText
	case t.hover:
		bg, fg = colRowHover, colText
	}
	c.fill(rect{0, 0, t.w, t.h}, bg)
	vis := rect{0, 0, t.w, t.visibleH(c) + c.px(1)}
	label := "Close"
	iw, gap := c.px(14), c.px(5)
	tw := c.textWidth(label, fontCaption)
	left := (t.w - iw - gap - tw) / 2
	c.icon(IconClose, rect{left, vis.Top, left + iw, vis.Bottom}, 14, fg)
	c.text(label, rect{left + iw + gap, vis.Top, left + iw + gap + tw + c.px(2), vis.Bottom}, fontCaption, fg, dtLeft)
}
