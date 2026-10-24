package ui

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// trayMenu is the small menu a right-click on the tray icon opens: "Show
// panel" and "Quit" side by side (two thirds and one third), so quitting is
// always one click away, whatever page the panel shows, and "Reset panel
// position" below them, for a panel moved away from the taskbar. It looks like the
// panel (its background, glow, buttons and drop shadow) but closes like a
// menu: when it loses the focus (a click anywhere else), on Esc, or after a
// choice.
type trayMenu struct {
	hwnd    uintptr
	shadow  *shadow
	fonts   *fonts
	scale   float64
	w, h    int32 // current size, device pixels
	actions [3]Action

	hover, press int // button index, -1 for none
	tracks       bool
	lastHidden   time.Time
}

const (
	trayMenuClass = "ResoundTrayMenu"
	trayMenuWL    = 248 // logical width of the menu
	trayMenuPadL  = 8   // around the buttons
	trayMenuGapL  = 8   // between the buttons
	trayMenuBtnHL = 34  // button height
)

func newTrayMenu(actions [3]Action) (*trayMenu, error) {
	m := &trayMenu{actions: actions, hover: -1, press: -1, scale: 1}
	if err := registerClass(trayMenuClass, 0, windows.NewCallback(m.wndProc)); err != nil {
		return nil, err
	}
	h, err := createWindow(wsExToolWindow|wsExTopmost, wsPopup, trayMenuClass)
	if err != nil {
		return nil, err
	}
	m.hwnd = h
	roundCorners(h) // like the panel
	if m.shadow, err = newShadow(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *trayMenu) visible() bool {
	r, _, _ := procIsWindowVisible.Call(m.hwnd)
	return r != 0
}

// recentlyHidden is true right after the menu closed, so the right-click on
// the tray icon that closed it (by taking the focus away) doesn't reopen it.
func (m *trayMenu) recentlyHidden() bool { return time.Since(m.lastHidden) < 300*time.Millisecond }

func (m *trayMenu) canvas(dc uintptr) *canvas { return &canvas{dc: dc, scale: m.scale, fonts: m.fonts} }

// show opens the menu next to pt (the cursor on the tray icon), placed like
// the panel: centred on it, next to the taskbar, inside the work area.
func (m *trayMenu) show(pt point) {
	work, scale := monitorAt(pt)
	if m.fonts == nil || m.fonts.scale != scale {
		if m.fonts != nil {
			m.fonts.destroy()
		}
		m.fonts = newFonts(scale)
		m.scale = scale
	}
	c := m.canvas(0)
	m.w, m.h = c.px(trayMenuWL), trayMenuHeight(c)
	m.hover, m.press = -1, -1
	gap := c.px(popupGapL)
	x := max(work.Left+gap, min(pt.X-m.w/2, work.Right-gap-m.w))
	y := work.Top + gap
	if pt.Y > work.Top+work.h()/2 {
		y = work.Bottom - gap - m.h
	}
	procSetWindowPos.Call(m.hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(m.w), uintptr(m.h), swpShowWindow)
	m.shadow.place(m.hwnd, rect{x, y, x + m.w, y + m.h}, m.scale)
	// The focus lets the menu close when the user clicks anywhere else. The
	// shell allows it: the click on the tray icon was this process's.
	procSetForegroundWindow.Call(m.hwnd)
	procInvalidateRect.Call(m.hwnd, 0, 0)
}

func (m *trayMenu) hide() {
	if !m.visible() {
		return
	}
	procShowWindow.Call(m.hwnd, swHide)
	m.shadow.hide()
	m.lastHidden = time.Now()
	m.hover, m.press, m.tracks = -1, -1, false
}

// trayMenuHeight is the menu's height: two rows of buttons.
func trayMenuHeight(c *canvas) int32 {
	return 2*c.px(trayMenuPadL) + 2*c.px(trayMenuBtnHL) + c.px(trayMenuGapL)
}

// boxes are the buttons: the first two side by side (two thirds and one
// third of the width), the third below them across the whole width.
func (m *trayMenu) boxes(c *canvas) [3]rect {
	pad, gap, bh := c.px(trayMenuPadL), c.px(trayMenuGapL), c.px(trayMenuBtnHL)
	in := rect{pad, pad, m.w - pad, m.h - pad}
	split := in.Left + (in.w()-gap)*2/3
	top := rect{in.Left, in.Top, in.Right, in.Top + bh}
	return [3]rect{
		{top.Left, top.Top, split, top.Bottom},
		{split + gap, top.Top, top.Right, top.Bottom},
		{in.Left, top.Bottom + gap, in.Right, top.Bottom + gap + bh},
	}
}

// hit returns the button at a client point, -1 for none.
func (m *trayMenu) hit(x, y int32) int {
	for i, b := range m.boxes(m.canvas(0)) {
		if x >= b.Left && x < b.Right && y >= b.Top && y < b.Bottom {
			return i
		}
	}
	return -1
}

func (m *trayMenu) wndProc(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case wmActivate:
		if loword(w) == waInactive {
			m.hide()
		}
		return 0
	case wmKeyDown:
		if w == vkEscape {
			m.hide()
		}
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		m.paint()
		return 0
	case wmMouseMove:
		if !m.tracks {
			tme := trackMouseEventStruct{Flags: tmeLeave, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			m.tracks = true
		}
		if i := m.hit(loword(l), hiword(l)); i != m.hover {
			m.hover = i
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmMouseLeave:
		m.tracks, m.hover = false, -1
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonDown:
		m.press = m.hit(loword(l), hiword(l))
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonUp:
		i, pressed := m.hit(loword(l), hiword(l)), m.press
		m.press = -1
		procInvalidateRect.Call(hwnd, 0, 0)
		if i >= 0 && i == pressed && !m.actions[i].Disabled {
			m.hide() // before the action, which may show the panel or quit
			if a := m.actions[i]; a.OnClick != nil {
				a.OnClick()
			}
		}
		return 0
	}
	return defWindowProc(hwnd, msg, w, l)
}

func (m *trayMenu) paint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(m.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(m.hwnd, uintptr(unsafe.Pointer(&ps)))
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(m.w), uintptr(m.h))
	old, _, _ := procSelectObject.Call(mem, bmp)
	defer func() {
		procSelectObject.Call(mem, old)
		procDeleteObject.Call(bmp)
		procDeleteDC.Call(mem)
	}()
	m.draw(m.canvas(mem))
	procBitBlt.Call(hdc, 0, 0, uintptr(m.w), uintptr(m.h), mem, 0, 0, 0x00CC0020 /* SRCCOPY */)
}

// draw paints the menu: the panel's background and glow, then its buttons.
func (m *trayMenu) draw(c *canvas) {
	c.fill(rect{0, 0, m.w, m.h}, colBackground)
	c.glows()
	for i, box := range m.boxes(c) {
		a := m.actions[i]
		paintButton(c, box, a.Label, a.Icon, a.Style, a.Disabled, m.hover == i, m.press == i)
	}
}
