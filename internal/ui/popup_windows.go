package ui

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	popupClass  = "ResoundPopup"
	popupWidthL = 340 // logical width
	popupPadL   = 8   // logical top/bottom padding
	popupGapL   = 12  // logical gap to the screen edge / taskbar
	// popupMinL is the minimum logical height, so switching pages doesn't
	// make the panel jump; short pages leave room at the bottom (or at a
	// Flex row). Taller pages grow it, up to the work area.
	popupMinL = 560
	// popupMaxL is the maximum logical height; longer pages scroll. Small
	// work areas lower it further, leaving room for the close tab.
	popupMaxL = 680
)

type slot struct {
	row  Row
	y, h int32 // content coordinates, device pixels
}

// Popup is the borderless panel shown when the tray icon is clicked. All of
// its methods must run on the UI thread (use Tray.Post from elsewhere).
type Popup struct {
	hwnd  uintptr
	rows  []Row
	slots []slot

	scale    float64
	fonts    *fonts
	width    int32
	height   int32
	contentH int32
	scroll   int32
	anchor   point

	hoverRow, hoverSeg int
	pressRow, pressSeg int
	tracking           bool
	lastHidden         time.Time

	measureDC uintptr // memory DC for measuring text outside painting

	shadow *shadow   // drop shadow window beneath the popup
	tab    *closeTab // the "Close" tab above its top right corner

	dd   *dropdownState // open dropdown list, if any
	drag *dragState     // slider being dragged, if any
	tip  tooltip

	// OnShow runs right before the popup becomes visible, so the app can
	// rebuild rows from fresh state.
	OnShow func()
	// OnKey receives keys and mouse buttons 3-5 while the popup has focus
	// (used to record a push-to-talk key). Returning true consumes it.
	OnKey func(KeyEvent) bool
	// OnDrop receives the paths of files and folders dropped on the popup
	// while it accepts drops (see SetAcceptDrops).
	OnDrop func(paths []string)

	drops bool
}

// KeyEvent is a key or mouse button press/release seen by the popup. VK is
// a Windows virtual-key code; Shift/Ctrl/Alt arrive as left/right variants.
type KeyEvent struct {
	VK   uint16
	Down bool
}

var procMapVirtualKeyW = user32.NewProc("MapVirtualKeyW")

// keyEvent converts a keyboard or mouse message into a KeyEvent. ok is false
// for messages that are not key/button events and for auto-repeats.
func keyEvent(m uint32, w, l uintptr) (KeyEvent, bool) {
	switch m {
	case wmKeyDown, wmSysKeyDown, wmKeyUp, wmSysKeyUp:
		down := m == wmKeyDown || m == wmSysKeyDown
		if down && l&(1<<30) != 0 {
			return KeyEvent{}, false // auto-repeat
		}
		vk := uint16(w)
		scan := (l >> 16) & 0xFF
		extended := l&(1<<24) != 0
		switch vk {
		case 0x10: // Shift: left/right only differ by scan code
			r, _, _ := procMapVirtualKeyW.Call(scan, 3 /* MAPVK_VSC_TO_VK_EX */)
			if r != 0 {
				vk = uint16(r)
			}
		case 0x11: // Ctrl
			vk = 0xA2
			if extended {
				vk = 0xA3
			}
		case 0x12: // Alt
			vk = 0xA4
			if extended {
				vk = 0xA5
			}
		}
		return KeyEvent{VK: vk, Down: down}, true
	case wmMButtonDown, wmMButtonUp:
		return KeyEvent{VK: 0x04, Down: m == wmMButtonDown}, true
	case wmXButtonDown, wmXButtonUp:
		vk := uint16(0x05) // XBUTTON1
		if hiword(w) == 2 {
			vk = 0x06 // XBUTTON2
		}
		return KeyEvent{VK: vk, Down: m == wmXButtonDown}, true
	}
	return KeyEvent{}, false
}

func newPopup() (*Popup, error) {
	p := &Popup{hoverRow: -1, pressRow: -1, scale: 1}
	p.measureDC, _, _ = procCreateCompatibleDC.Call(0)
	if err := registerClass(popupClass, 0, windows.NewCallback(p.wndProc)); err != nil {
		return nil, err
	}
	h, err := createWindow(wsExToolWindow|wsExTopmost, wsPopup, popupClass)
	if err != nil {
		return nil, err
	}
	p.hwnd = h
	if procDwmSetWindowAttribute.Find() == nil { // rounded corners on Windows 11
		pref := uint32(dwmwcpRound)
		procDwmSetWindowAttribute.Call(h, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&pref)), 4)
	}
	// The drop shadow is a window of its own (CS_DROPSHADOW is too faint).
	if p.shadow, err = newShadow(); err != nil {
		return nil, err
	}
	if p.tab, err = newCloseTab(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Visible reports whether the popup is shown.
func (p *Popup) Visible() bool {
	r, _, _ := procIsWindowVisible.Call(p.hwnd)
	return r != 0
}

// SetRows replaces the content. If the popup is open it is re-laid out in
// place, keeping its anchor.
func (p *Popup) SetRows(rows []Row) {
	p.rows = rows
	if p.Visible() {
		p.layoutAndPlace()
	}
}

// Repaint redraws the open popup without rebuilding its rows, for rows that
// read live state while painting (a progress bar).
func (p *Popup) Repaint() {
	if p.Visible() {
		procInvalidateRect.Call(p.hwnd, 0, 0)
	}
}

// ScrollToTop resets the scroll position, e.g. when switching pages.
func (p *Popup) ScrollToTop() {
	p.closeDropdown()
	p.scroll = 0
	procInvalidateRect.Call(p.hwnd, 0, 0)
}

// ShowAt opens the popup next to pt (normally the cursor on the tray icon).
func (p *Popup) ShowAt(pt point) {
	p.anchor = pt
	p.scroll = 0
	if p.OnShow != nil {
		p.OnShow()
	}
	p.layoutAndPlace()
	procSetForegroundWindow.Call(p.hwnd)
}

// Hide closes the popup.
func (p *Popup) Hide() {
	if !p.Visible() {
		return
	}
	p.dd = nil
	p.hideTip()
	procShowWindow.Call(p.hwnd, swHide)
	p.shadow.hide()
	p.tab.hide()
	p.lastHidden = time.Now()
	p.endDrag() // after hiding, so its re-render doesn't show the popup again
	p.hoverRow, p.pressRow = -1, -1
	p.tracking = false
}

// recentlyHidden is true right after the popup closed, so the click on the
// tray icon that closed it (by deactivation) does not reopen it.
func (p *Popup) recentlyHidden() bool { return time.Since(p.lastHidden) < 300*time.Millisecond }

func (p *Popup) layoutAndPlace() {
	work, scale := monitorAt(p.anchor)
	if p.fonts == nil || p.fonts.scale != scale {
		if p.fonts != nil {
			p.fonts.destroy()
		}
		p.fonts = newFonts(scale)
		p.scale = scale
	}
	// A DC is needed to measure rows whose height follows their text.
	dc, _, _ := procGetDC.Call(p.hwnd)
	c := p.canvas(dc)
	p.width = c.px(popupWidthL)

	p.slots = p.slots[:0]
	var y int32
	for _, r := range p.rows {
		h := c.px(r.height())
		if m, ok := r.(measurer); ok && dc != 0 {
			h = m.measure(c, p.width)
		}
		p.slots = append(p.slots, slot{row: r, y: y, h: h})
		y += h
	}
	if dc != 0 {
		procReleaseDC.Call(p.hwnd, dc)
	}
	c = p.canvas(0)
	gap := c.px(popupGapL)
	tabRoom := c.px(closeTabHL - closeTabUnderL)
	maxH := min(c.px(popupMaxL), work.h()-2*gap-tabRoom)
	if minH := min(c.px(popupMinL), maxH) - 2*c.px(popupPadL); y < minH {
		p.grow(minH - y)
		y = minH
	}
	p.contentH = y
	defer p.syncDropdown() // keep an open list attached to its row

	p.height = min(p.contentH+2*c.px(popupPadL), maxH)
	p.scroll = max(0, min(p.scroll, p.maxScroll()))

	// Horizontally centred on the anchor, kept inside the work area.
	x := p.anchor.X - p.width/2
	x = max(work.Left+gap, min(x, work.Right-gap-p.width))
	// Next to the taskbar: above it when the anchor is in the lower half.
	var yPos int32
	if p.anchor.Y > work.Top+work.h()/2 {
		yPos = work.Bottom - gap - p.height
	} else {
		yPos = work.Top + gap + tabRoom
	}
	procSetWindowPos.Call(p.hwnd, hwndTopmost, uintptr(x), uintptr(yPos), uintptr(p.width), uintptr(p.height), swpShowWindow)
	p.shadow.place(p.hwnd, rect{x, yPos, x + p.width, yPos + p.height}, p.scale)
	p.tab.place(rect{x, yPos, x + p.width, yPos + p.height}) // between the popup and its shadow
	procInvalidateRect.Call(p.hwnd, 0, 0)
}

func (p *Popup) maxScroll() int32 {
	c := p.canvas(0)
	return max(0, p.contentH+2*c.px(popupPadL)-p.height)
}

// canvas returns a canvas on dc, or, for dc 0, on the popup's measuring DC:
// hit testing and tooltips need text widths too (a badge's label), or
// their segments come out narrower than what was painted.
func (p *Popup) canvas(dc uintptr) *canvas {
	if dc == 0 {
		dc = p.measureDC
	}
	return &canvas{dc: dc, scale: p.scale, fonts: p.fonts}
}

// rowRect returns slot i's rectangle in window coordinates.
func (p *Popup) rowRect(i int) rect {
	top := p.slots[i].y - p.scroll + p.canvas(0).px(popupPadL)
	return rect{0, top, p.width, top + p.slots[i].h}
}

// hitTest maps a window point to (row, segment); -1 when nothing is hit.
func (p *Popup) hitTest(x, y int32) (int, int) {
	c := p.canvas(0)
	for i, s := range p.slots {
		r := p.rowRect(i)
		if y < r.Top || y >= r.Bottom {
			continue
		}
		for seg, sr := range s.row.segments(c, r) {
			if x >= sr.Left && x < sr.Right && y >= sr.Top && y < sr.Bottom {
				return i, seg
			}
		}
		return -1, -1
	}
	return -1, -1
}

func (p *Popup) setHover(row, seg int) {
	if row != p.hoverRow || seg != p.hoverSeg {
		p.hoverRow, p.hoverSeg = row, seg
		procInvalidateRect.Call(p.hwnd, 0, 0)
	}
}

func (p *Popup) wndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	if ev, ok := keyEvent(m, w, l); ok && p.OnKey != nil && p.OnKey(ev) {
		if m == wmXButtonDown || m == wmXButtonUp {
			return 1 // documented return value for X buttons
		}
		return 0
	}
	switch m {
	case wmTimer:
		if w == tipTimerID {
			p.showTip()
		}
		return 0
	case wmActivate:
		// The popup stays open when another window gets the focus: closing
		// is a deliberate click on its close tab, Esc or the tray icon.
		return 0
	case wmDropFiles:
		paths := droppedFiles(w)
		if len(paths) > 0 && p.OnDrop != nil {
			p.OnDrop(paths) // may call SetRows
		}
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		p.paint()
		return 0
	case wmMouseMove:
		if !p.tracking {
			tme := trackMouseEventStruct{Flags: tmeLeave, Track: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			p.tracking = true
		}
		if p.drag != nil {
			p.dragTo(loword(l), false)
			return 0
		}
		if p.dd != nil {
			p.dropdownMessage(m, w, l)
			return 0
		}
		row, seg := p.hitTest(loword(l), hiword(l))
		p.setHover(row, seg)
		p.armTip(p.rowTipKey(row, seg))
		return 0
	case wmMouseLeave:
		p.tracking = false
		p.setHover(-1, -1)
		p.hideTip()
		if p.dd != nil && p.dd.hover >= 0 {
			p.dd.hover = -1
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmCaptureChanged:
		p.endDrag() // capture taken away mid-drag: keep the last value
		return 0
	case wmLButtonUp:
		if p.drag != nil {
			p.dragTo(loword(l), false)
			p.endDrag()
			return 0
		}
	}
	switch m {
	case wmLButtonDown, wmLButtonUp, wmMouseWheel, wmKeyDown:
		if p.dropdownMessage(m, w, l) {
			return 0
		}
	}
	switch m {
	case wmLButtonDown:
		p.pressRow, p.pressSeg = p.hitTest(loword(l), hiword(l))
		p.startDrag(loword(l))
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonUp:
		row, seg := p.hitTest(loword(l), hiword(l))
		pressedRow, pressedSeg := p.pressRow, p.pressSeg
		p.pressRow = -1
		procInvalidateRect.Call(hwnd, 0, 0)
		if row >= 0 && row == pressedRow && seg == pressedSeg {
			r := p.slots[row].row
			p.hideTip()
			if fh, ok := r.(fieldHost); ok {
				if f, ok := fh.field(seg); ok {
					p.openDropdown(row, seg, f)
					return 0
				}
			}
			keep := r.keepsOpen()
			if k, ok := r.(segmentKeeper); ok {
				keep = k.keepsOpenAt(seg)
			}
			if !keep {
				p.Hide()
			}
			r.click(seg) // may call SetRows; nothing below touches slots
		}
		return 0
	case wmMouseWheel:
		delta := hiword(w) // multiples of 120 per notch
		p.scroll = max(0, min(p.scroll-delta*p.canvas(0).px(40)/120, p.maxScroll()))
		p.hideTip()
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmKeyDown:
		if w == vkEscape {
			p.Hide()
		}
		return 0
	}
	return defWindowProc(hwnd, m, w, l)
}

func (p *Popup) paint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(p.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(p.hwnd, uintptr(unsafe.Pointer(&ps)))

	// Double buffering avoids flicker on hover repaints.
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(p.width), uintptr(p.height))
	oldBmp, _, _ := procSelectObject.Call(mem, bmp)
	defer func() {
		procSelectObject.Call(mem, oldBmp)
		procDeleteObject.Call(bmp)
		procDeleteDC.Call(mem)
	}()

	c := p.canvas(mem)
	c.fill(rect{0, 0, p.width, p.height}, colBackground)
	c.glows()
	for i, s := range p.slots {
		r := p.rowRect(i)
		if r.Bottom < 0 || r.Top > p.height {
			continue
		}
		st := rowState{hover: -1, press: -1, open: -1}
		if i == p.hoverRow {
			st.hover = p.hoverSeg
		}
		if i == p.pressRow {
			st.press = p.pressSeg
		}
		if p.dd != nil && p.dd.row == i {
			st.open = p.dd.seg
		}
		s.row.paint(c, r, st)
	}
	// Scroll indicator.
	if ms := p.maxScroll(); ms > 0 {
		track := p.height - 2*c.px(popupPadL)
		thumb := max(c.px(24), track*p.height/(p.contentH+2*c.px(popupPadL)))
		top := c.px(popupPadL) + (track-thumb)*p.scroll/ms
		c.roundRect(rect{p.width - c.px(5), top, p.width - c.px(2), top + thumb}, c.px(3), colControl)
	}
	p.paintDropdown(c)
	p.paintTip(c)
	procBitBlt.Call(hdc, 0, 0, uintptr(p.width), uintptr(p.height), mem, 0, 0, 0x00CC0020 /* SRCCOPY */)
}

// rowTipKey identifies a hovered row segment that can show a tooltip.
func (p *Popup) rowTipKey(row, seg int) string {
	if row < 0 || row >= len(p.slots) {
		return ""
	}
	if _, ok := p.slots[row].row.(tipper); !ok {
		return ""
	}
	return fmt.Sprintf("row:%d:%d:%p", row, seg, p.slots[row].row)
}

// measurer is implemented by rows whose height follows their content, such
// as wrapped text. It returns the height in device pixels for the panel
// width; height() is only the fallback when nothing can be measured.
type measurer interface {
	measure(c *canvas, width int32) int32
}

// segmentKeeper is implemented by rows whose segments differ in whether a
// click closes the popup; it overrides keepsOpen per segment.
type segmentKeeper interface {
	keepsOpenAt(seg int) bool
}

// dragger is implemented by rows with segments that follow the mouse while
// the left button is held, such as a slider's track. Rows are rebuilt on
// every change, so an ongoing drag finds its row again by its id.
type dragger interface {
	// dragIDAt returns the drag id of segment seg, "" if it doesn't drag.
	dragIDAt(seg int) string
	// dragTo moves drag id to window x; done is true once, when it ends.
	dragTo(c *canvas, r rect, id string, x int32, done bool)
}

type dragState struct {
	id string
	x  int32 // last mouse x, for ending the drag without a position
}

var (
	procSetCapture     = user32.NewProc("SetCapture")
	procReleaseCapture = user32.NewProc("ReleaseCapture")
)

// startDrag begins a drag when the pressed segment is a dragger's.
func (p *Popup) startDrag(x int32) {
	if p.pressRow < 0 {
		return
	}
	d, ok := p.slots[p.pressRow].row.(dragger)
	if !ok || d.dragIDAt(p.pressSeg) == "" {
		return
	}
	p.drag = &dragState{id: d.dragIDAt(p.pressSeg), x: x}
	p.hideTip()
	procSetCapture.Call(p.hwnd)
	p.dragTo(x, false)
}

// dragTo forwards a mouse position to the dragged row, wherever the last
// re-render put it.
func (p *Popup) dragTo(x int32, done bool) {
	if p.drag == nil {
		return
	}
	p.drag.x = x
	c := p.canvas(0)
	for i, s := range p.slots {
		d, ok := s.row.(dragger)
		if !ok {
			continue
		}
		for seg := range s.row.segments(c, p.rowRect(i)) {
			if d.dragIDAt(seg) == p.drag.id {
				p.pressRow, p.pressSeg = i, seg
				d.dragTo(c, p.rowRect(i), p.drag.id, x, done) // may call SetRows
				return
			}
		}
	}
}

// endDrag finishes a drag at its last position. Safe to call when idle.
func (p *Popup) endDrag() {
	if p.drag == nil {
		return
	}
	p.dragTo(p.drag.x, true)
	p.drag = nil
	p.pressRow = -1
	procReleaseCapture.Call() // its WM_CAPTURECHANGED finds no drag
	procInvalidateRect.Call(p.hwnd, 0, 0)
}

// grow gives extra height to the first Flex row, moving the rows after it
// down; without one, the space stays empty at the bottom.
func (p *Popup) grow(extra int32) {
	for i := range p.slots {
		if _, ok := p.slots[i].row.(flexRow); !ok {
			continue
		}
		p.slots[i].h += extra
		for j := i + 1; j < len(p.slots); j++ {
			p.slots[j].y += extra
		}
		return
	}
}

// --- pinning and file drops --------------------------------------------------

const wmDropFiles = 0x0233

var (
	procDragAcceptFiles = shell32.NewProc("DragAcceptFiles")
	procDragQueryFileW  = shell32.NewProc("DragQueryFileW")
	procDragFinish      = shell32.NewProc("DragFinish")
)

// SetAcceptDrops makes the popup accept dropped files and folders
// (OnDrop), e.g. on the sound library page.
func (p *Popup) SetAcceptDrops(on bool) {
	if p.drops == on {
		return
	}
	p.drops = on
	accept := uintptr(0)
	if on {
		accept = 1
	}
	procDragAcceptFiles.Call(p.hwnd, accept)
}

// Hwnd returns the popup window, e.g. as the owner of a dialog so the
// dialog opens above the always-on-top panel.
func (p *Popup) Hwnd() uintptr { return p.hwnd }

// droppedFiles reads and releases a WM_DROPFILES handle.
func droppedFiles(hdrop uintptr) []string {
	defer procDragFinish.Call(hdrop)
	n, _, _ := procDragQueryFileW.Call(hdrop, 0xFFFFFFFF, 0, 0)
	var out []string
	for i := uintptr(0); i < n; i++ {
		size, _, _ := procDragQueryFileW.Call(hdrop, i, 0, 0)
		buf := make([]uint16, size+1)
		procDragQueryFileW.Call(hdrop, i, uintptr(unsafe.Pointer(&buf[0])), size+1)
		out = append(out, windows.UTF16ToString(buf))
	}
	return out
}
