package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// panelTab is the tab that peeks out above the popup across its whole
// width: a handle on the left that moves the panel ("Drag to move"; a
// double-click docks it at the taskbar again) and "Close" on the right. The
// popup stays open when another window gets the focus, so closing it is a
// deliberate click here (or Esc, or the tray icon). The tab is a window of
// its own, right below the popup in the z-order: the popup covers its lower
// edge, so it looks tucked under the panel. It never takes the focus.
//
// Like the popup it is rounded by Windows 11 itself (anti-aliased, the same
// radius), and it reaches far enough under the popup that its own lower
// corners stay hidden behind the popup's rounded ones. Where Windows can't
// round it (Windows 10, square popup), a window region rounds it.
type panelTab struct {
	hwnd    uintptr
	popup   *Popup
	w, h    int32 // current size, device pixels
	rounded bool  // Windows rounds its corners; else a region does

	hover  int  // tabClose, tabHandle or -1
	press  int  // the part pressed, -1 for none
	tracks bool // TrackMouseEvent is armed
	// While the handle is held: where it was pressed (screen pixels), and
	// whether the mouse went further than a click, which moves the panel.
	pressAt point
	moving  bool
}

const (
	panelTabClass = "ResoundPanelTab"
	panelTabHL    = 48
	// panelTabUnderL is the part hidden under the popup: more than both
	// windows' corner radii (8), so no corner of the tab shows in the
	// popup's rounded top corners.
	panelTabUnderL = 24
	panelTabPadL   = 12 // between the content and the tab's sides
)

// The tab's parts.
const (
	tabHandle = iota
	tabClose
)

const (
	csDblClks        = 0x0008
	wmLButtonDblClk  = 0x0203
	wmSetCursor      = 0x0020
	wmMouseActivate  = 0x0021
	maNoActivate     = 3
	idcSizeAll       = 32646
	smCxDrag         = 68
	smCyDrag         = 69
	htClient         = 1
	wmAppCommand     = 0x0319
	appCommandBack   = 1 // APPCOMMAND_BROWSER_BACKWARD
	faAppCommandMask = 0xF000
)

var (
	procSetWindowRgn     = user32.NewProc("SetWindowRgn")
	procSetCursor        = user32.NewProc("SetCursor")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetMessagePos    = user32.NewProc("GetMessagePos")
)

// newPanelTab creates the tab, owned by owner (the popup's shadow).
func newPanelTab(p *Popup, owner uintptr) (*panelTab, error) {
	t := &panelTab{popup: p, hover: -1, press: -1}
	// CS_DBLCLKS: a double-click on the handle resets the position.
	if err := registerClass(panelTabClass, csDblClks, windows.NewCallback(t.wndProc)); err != nil {
		return nil, err
	}
	h, err := createOwnedWindow(wsExToolWindow|wsExTopmost|wsExNoActivate, wsPopup, panelTabClass, owner)
	if err != nil {
		return nil, err
	}
	t.hwnd = h
	t.rounded = roundCorners(h)
	return t, nil
}

// visibleH is the height of the part above the popup, device pixels.
func (t *panelTab) visibleH(c *canvas) int32 { return c.px(panelTabHL - panelTabUnderL) }

// size sets the tab's size for a popup w wide, and its rounded region
// where Windows doesn't round it.
func (t *panelTab) size(c *canvas, w int32) {
	h := c.px(panelTabHL)
	if w == t.w && h == t.h {
		return
	}
	t.w, t.h = w, h
	if t.rounded {
		return
	}
	radius := c.px(20)
	rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), uintptr(radius), uintptr(radius))
	procSetWindowRgn.Call(t.hwnd, rgn, 1) // the window owns the region now
}

// origin is the tab's top left corner above popup rectangle r.
func (t *panelTab) origin(c *canvas, r rect) point {
	return point{r.Left, r.Top - t.visibleH(c)}
}

// place shows the tab above popup rectangle r (screen pixels), as wide as
// the popup, right below it in the z-order. Call it after placing the
// shadow, so the tab sits between the two.
func (t *panelTab) place(r rect) {
	c := t.popup.canvas(0)
	t.size(c, r.w())
	o := t.origin(c, r)
	procSetWindowPos.Call(t.hwnd, t.popup.hwnd, uintptr(o.X), uintptr(o.Y), uintptr(t.w), uintptr(t.h), swpNoActivate|swpShowWindow)
	procInvalidateRect.Call(t.hwnd, 0, 0)
}

func (t *panelTab) hide() {
	if t.press == tabHandle {
		procReleaseCapture.Call() // its WM_CAPTURECHANGED ends a move
	}
	t.hover, t.press, t.moving = -1, -1, false
	procShowWindow.Call(t.hwnd, swHide)
}

// closeBox is the "Close" part at the right end; the rest of the visible
// part is the handle.
func (t *panelTab) closeBox(c *canvas) rect {
	x := t.w - c.px(panelTabPadL) - t.closeWidth(c) - c.px(8)
	return rect{x, 0, t.w, t.visibleH(c)}
}

// hit returns the part at a client point, -1 for none (the part under the
// popup, which the popup covers anyway).
func (t *panelTab) hit(x, y int32) int {
	c := t.popup.canvas(0)
	if x < 0 || x >= t.w || y < 0 || y >= t.visibleH(c) {
		return -1
	}
	if x >= t.closeBox(c).Left {
		return tabClose
	}
	return tabHandle
}

// messagePos is the cursor's screen position when the current message was
// sent. The tab moves under the cursor while dragging, so its client
// coordinates would be relative to where it was.
func messagePos() point {
	r, _, _ := procGetMessagePos.Call()
	return point{loword(r), hiword(r)}
}

func (t *panelTab) wndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmMouseActivate:
		return maNoActivate // clicking the tab doesn't take the focus
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		t.paint()
		return 0
	case wmSetCursor:
		if loword(l) == htClient && t.hover == tabHandle {
			cur, _, _ := procLoadCursorW.Call(0, idcSizeAll)
			procSetCursor.Call(cur)
			return 1
		}
	case wmAppCommand: // the mouse's back button over the tab
		return t.popup.wndProc(t.popup.hwnd, m, w, l)
	case wmMouseMove:
		if !t.tracks {
			tme := trackMouseEventStruct{Flags: tmeLeave, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			t.tracks = true
		}
		if t.press == tabHandle {
			t.dragTo(messagePos())
			return 0
		}
		if part := t.hit(loword(l), hiword(l)); part != t.hover {
			t.hover = part
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmMouseLeave:
		t.tracks = false
		if t.press != tabHandle { // a held handle keeps the mouse captured
			t.hover, t.press = -1, -1
		}
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonDown, wmLButtonDblClk:
		t.press = t.hit(loword(l), hiword(l))
		switch {
		case t.press == tabHandle && m == wmLButtonDblClk:
			t.press = -1
			t.popup.ResetPosition()
		case t.press == tabHandle:
			t.pressAt, t.moving = messagePos(), false
			procSetCapture.Call(hwnd)
		}
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonUp:
		pressed := t.press
		t.press = -1
		switch {
		case pressed == tabHandle:
			procReleaseCapture.Call() // its WM_CAPTURECHANGED ends the move
		case pressed == tabClose && t.hit(loword(l), hiword(l)) == tabClose:
			t.popup.Hide()
			return 0
		}
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmCaptureChanged:
		// Released after a drag, or taken away (by another window, Esc,
		// hiding): the panel stays where it is now, unless Esc put it back.
		if t.moving {
			t.moving = false
			t.popup.endMove(true)
		}
		if t.press == tabHandle {
			t.press = -1
		}
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

// dragTo follows the mouse while the handle is held. The panel only starts
// moving once the mouse went past the system's drag distance, so a click or
// a double-click doesn't nudge it.
func (t *panelTab) dragTo(pt point) {
	if !t.moving {
		dx, _, _ := procGetSystemMetrics.Call(smCxDrag)
		dy, _, _ := procGetSystemMetrics.Call(smCyDrag)
		if abs32(pt.X-t.pressAt.X) < int32(dx) && abs32(pt.Y-t.pressAt.Y) < int32(dy) {
			return
		}
		t.moving = true
		t.popup.beginMove(t.pressAt)
	}
	t.popup.moveTo(pt)
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (t *panelTab) paint() {
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

const (
	tabCloseLabel = "CLOSE"
	tabCloseKey   = "ESC"
	tabMoveLabel  = "DRAG TO MOVE"
)

// closeWidth is the width of ✕ CLOSE [ESC].
func (t *panelTab) closeWidth(c *canvas) int32 {
	iw, gap := c.px(12), c.px(4)
	return iw + gap + c.textWidth(tabCloseLabel, fontSmall) + c.px(6) + t.keyWidth(c)
}

func (t *panelTab) keyWidth(c *canvas) int32 { return c.textWidth(tabCloseKey, fontSmall) + c.px(8) }

// draw paints the tab in the part above the popup: the handle (six dots and
// "Drag to move") on the left, ✕ CLOSE [ESC] on the right, each lighting up
// under the mouse.
func (t *panelTab) draw(c *canvas) {
	// Lighter than the panel, so it reads as a card peeking out.
	c.fill(rect{0, 0, t.w, t.h}, colControl)
	vis := rect{0, 0, t.w, t.visibleH(c) + c.px(1)}
	pad := c.px(panelTabPadL)
	// Everything is centred on one line, the ESC keycap's: centring the
	// labels in vis instead, an odd height away, put them a pixel lower.
	kh := c.px(14)
	line := rect{0, vis.Top + (vis.h()-kh)/2, t.w, vis.Top + (vis.h()-kh)/2 + kh}

	// The handle.
	fg := colMuted
	if t.hover == tabHandle || t.press == tabHandle {
		fg = colText
	}
	iw := c.px(16)
	c.icon(IconGrip, rect{pad, line.Top, pad + iw, line.Bottom}, 16, fg)
	x := pad + iw + c.px(6)
	c.text(tabMoveLabel, rect{x, line.Top, t.closeBox(c).Left, line.Bottom}, fontSmall, fg, dtLeft)

	// Close, with a button face of its own.
	box := t.closeBox(c)
	bg := colControl
	switch {
	case t.press == tabClose:
		bg = shade(colControl, -18)
	case t.hover == tabClose:
		bg = shade(colControl, 14)
	}
	face := rect{box.Left, vis.Top + c.px(3), box.Right - c.px(4), vis.Bottom - c.px(3)}
	c.roundRect(face, c.px(14), bg)
	x = box.Right - pad - t.closeWidth(c)
	cw := c.px(12)
	// The X sat a pixel high beside the capitals; nudged down to them.
	c.icon(IconClose, rect{x, line.Top + c.px(1), x + cw, line.Bottom + c.px(1)}, 12, colText)
	x += cw + c.px(4)
	tw := c.textWidth(tabCloseLabel, fontSmall)
	c.text(tabCloseLabel, rect{x, line.Top, x + tw + c.px(2), line.Bottom}, fontSmall, colText, dtLeft)
	x += tw + c.px(6)
	keycap := rect{x, line.Top, x + t.keyWidth(c), line.Bottom}
	// Its background loses a pixel at the top, which evens out the space
	// around the letters; they stay where they are.
	c.roundRect(rect{keycap.Left, keycap.Top + c.px(1), keycap.Right, keycap.Bottom}, c.px(6), shade(bg, -22))
	c.text(tabCloseKey, keycap, fontSmall, colMuted, dtCenter)
}
