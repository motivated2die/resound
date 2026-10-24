package ui

import "unsafe"

// Where the panel shows. By default it is docked: next to the tray icon,
// centred on it and against the taskbar, wherever the taskbar is. The user
// can drag it anywhere by its tab (paneltab_windows.go); that position is
// kept (Popup.SetPosition, OnMove) until it is reset. Either way the panel
// and its tab always lie inside one monitor's work area, so no part of it
// can end up off screen or under the taskbar.

// PanelPos is a position the user moved the panel to: the top left corner
// of the panel (below its tab), in screen pixels.
type PanelPos struct{ X, Y int32 }

const monitorDefaultToNull = 0

// monitorInfoAt returns the work area and DPI scale of the monitor that
// contains pt; ok is false when no monitor does (a screen was unplugged,
// the arrangement or resolution changed).
func monitorInfoAt(pt point) (work rect, scale float64, ok bool) {
	mon, _, _ := procMonitorFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&pt))), monitorDefaultToNull)
	if mon == 0 {
		return rect{}, 1, false
	}
	work, scale = monitorAt(pt)
	return work, scale, true
}

// dockedPos places a panel of w×h next to anchor (normally the tray icon):
// horizontally centred on it, gap away from the work area's edges, above
// the taskbar when the anchor is in the lower half, else below it with room
// for the tab (tabH) above the panel.
func dockedPos(anchor point, work rect, w, h, gap, tabH int32) point {
	x := anchor.X - w/2
	x = max(work.Left+gap, min(x, work.Right-gap-w))
	y := work.Top + gap + tabH
	if anchor.Y > work.Top+work.h()/2 {
		y = work.Bottom - gap - h
	}
	return point{x, y}
}

// clampPanel moves a panel of w×h at pos, with its tab of height tabH above
// it, fully into work. A panel taller than the work area keeps its tab
// visible.
func clampPanel(pos point, work rect, w, h, tabH int32) point {
	x := max(work.Left, min(pos.X, work.Right-w))
	y := max(work.Top+tabH, min(pos.Y, work.Bottom-h))
	return point{x, y}
}

// moveState is a move of the panel by its tab.
type moveState struct {
	cursor     point   // the mouse, screen pixels
	offX, offY float64 // from the panel's corner to the mouse, logical pixels
	from       *point  // the position before the move, for Esc
}

// SetPosition puts the panel where the user moved it before (nil: docked
// at the taskbar), e.g. from the config at start. It doesn't call OnMove.
func (p *Popup) SetPosition(pos *PanelPos) {
	p.custom = nil
	if pos != nil {
		p.custom = &point{pos.X, pos.Y}
	}
	if p.Visible() {
		p.layoutAndPlace()
	}
}

// Moved reports whether the user moved the panel away from the taskbar.
func (p *Popup) Moved() bool { return p.custom != nil }

// ResetPosition docks the panel at the taskbar again, where it opens by
// default, and tells OnMove.
func (p *Popup) ResetPosition() {
	p.cancelMove()
	if p.custom == nil {
		return
	}
	p.custom = nil
	if p.dockAnchor != nil {
		p.anchor = p.dockAnchor()
	}
	if p.Visible() {
		p.layoutAndPlace()
	}
	if p.OnMove != nil {
		p.OnMove(nil)
	}
}

// beginMove starts moving the panel by its tab, pressed at cursor.
func (p *Popup) beginMove(cursor point) {
	p.closeDropdown()
	p.hideTip()
	var from *point
	if p.custom != nil {
		f := *p.custom
		from = &f
	}
	p.move = &moveState{
		cursor: cursor,
		offX:   float64(cursor.X-p.pos.X) / p.scale,
		offY:   float64(cursor.Y-p.pos.Y) / p.scale,
		from:   from,
	}
}

// moveTo moves the panel with the mouse, kept inside the work area of the
// screen under it. On another screen it is laid out for that screen first:
// its scale and height may differ.
func (p *Popup) moveTo(cursor point) {
	if p.move == nil {
		return
	}
	p.move.cursor = cursor
	if work, scale := monitorAt(cursor); work != p.work || scale != p.scale {
		p.layoutAndPlace()
		return
	}
	if pos := p.target(false); pos != p.pos {
		p.pos = pos
		p.moveWindows()
	}
}

// endMove ends a move: the panel stays where it is now (commit, told to
// OnMove), or goes back to where it was (Esc). Safe to call when idle.
func (p *Popup) endMove(commit bool) {
	m := p.move
	if m == nil {
		return
	}
	p.move = nil
	if !commit {
		p.custom = m.from
		if p.Visible() {
			p.layoutAndPlace()
		}
		return
	}
	pos := p.pos
	p.custom = &pos
	if p.OnMove != nil {
		p.OnMove(&PanelPos{pos.X, pos.Y})
	}
}

// cancelMove puts a panel being moved back where it was (Esc).
func (p *Popup) cancelMove() {
	if p.move == nil {
		return
	}
	p.endMove(false)
	procReleaseCapture.Call() // the tab's WM_CAPTURECHANGED finds no move
}

var (
	procBeginDeferWindowPos = user32.NewProc("BeginDeferWindowPos")
	procDeferWindowPos      = user32.NewProc("DeferWindowPos")
	procEndDeferWindowPos   = user32.NewProc("EndDeferWindowPos")
)

// moveWindows moves the popup, its tab and its shadow to p.pos in one
// step, so neither lags behind while dragging. Their size and order stay.
func (p *Popup) moveWindows() {
	c := p.canvas(0)
	r := rect{p.pos.X, p.pos.Y, p.pos.X + p.width, p.pos.Y + p.height}
	moves := []struct {
		hwnd uintptr
		at   point
	}{
		{p.hwnd, p.pos},
		{p.tab.hwnd, p.tab.origin(c, r)},
		{p.shadow.hwnd, p.shadow.origin(r)},
	}
	const flags = swpNoSize | swpNoZOrder | swpNoActivate
	dwp, _, _ := procBeginDeferWindowPos.Call(uintptr(len(moves)))
	for _, m := range moves {
		if dwp != 0 {
			dwp, _, _ = procDeferWindowPos.Call(dwp, m.hwnd, 0, uintptr(m.at.X), uintptr(m.at.Y), 0, 0, flags)
		}
	}
	if dwp != 0 && func() bool { r, _, _ := procEndDeferWindowPos.Call(dwp); return r != 0 }() {
		return
	}
	for _, m := range moves { // one by one when Windows can't defer them
		procSetWindowPos.Call(m.hwnd, 0, uintptr(m.at.X), uintptr(m.at.Y), 0, 0, flags)
	}
}

// Screens and the taskbar can change while the panel is open: a screen
// unplugged or added, a new resolution or arrangement, another scale, the
// taskbar moved or resized. The panel is placed again then, by the rules
// above: it moves back onto its screen's work area, or docks at the
// taskbar when its screen is gone.
const (
	wmDisplayChange = 0x007E
	wmSettingChange = 0x001A
	wmDpiChanged    = 0x02E0
	spiSetWorkArea  = 0x002F
	// wmPlaceAgain is posted for such a change, so the popup is placed
	// once Windows has finished updating its screens.
	wmPlaceAgain = wmApp + 22
)

// screenChanged handles the messages above. It returns true when the
// message is done with; DefWindowProc still gets the broadcasts.
func (p *Popup) screenChanged(m uint32, w uintptr) bool {
	switch {
	case m == wmDisplayChange, m == wmSettingChange && w == spiSetWorkArea:
		procPostMessageW.Call(p.hwnd, wmPlaceAgain, 0, 0)
		return false
	case m == wmDpiChanged: // placed by the rules above, not Windows' suggestion
		procPostMessageW.Call(p.hwnd, wmPlaceAgain, 0, 0)
		return true
	case m == wmPlaceAgain:
		// Moving the popup to a screen of another scale sends WM_DPICHANGED
		// too; placing it again there changes nothing. While it is dragged,
		// each mouse move places it anyway.
		if p.Visible() && p.move == nil {
			p.layoutAndPlace()
		}
		return true
	}
	return false
}
