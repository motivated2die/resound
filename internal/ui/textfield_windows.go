package ui

// TextField is a one-line text input in the panel. The row paints the
// field's box; typing happens in a native EDIT control placed over it, so
// selection, the clipboard, IME and undo work as everywhere in Windows.
// Rows are rebuilt on every render, so the control is found again by its
// row's id and keeps what the user typed until the row disappears.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW      = user32.NewProc("CallWindowProcW")
	procSendMessageW         = user32.NewProc("SendMessageW")
	procSetFocus             = user32.NewProc("SetFocus")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procSetBkColor           = gdi32.NewProc("SetBkColor")
)

const (
	wsChild        = 0x40000000
	wsClipChildren = 0x02000000
	esAutoHScroll  = 0x0080
	gwlpWndProc    = ^uintptr(3) // -4
	wmSetFont      = 0x0030
	wmChar         = 0x0102
	wmCtlColorEdit = 0x0133
	emSetSel       = 0x00B1
	emSetLimitText = 0x00C5
	emSetMargins   = 0x00D3
	ecLeftRight    = 0x0003

	// wmFieldKey is posted to the popup when Enter (w=1) or Esc (w=2) is
	// pressed in the field. Posted, not handled in the control's own
	// window procedure, because the callback may destroy the control.
	wmFieldKey = wmApp + 21

	textFieldH = 38
)

type textFieldRow struct {
	id       string
	value    string
	maxLen   int
	onSubmit func(text string)
	onCancel func()
}

// TextField is a one-line input, pre-filled with value (selected, so typing
// replaces it) and focused when it appears. Enter calls onSubmit with the
// text, Esc calls onCancel. id must be stable across re-renders; a new id
// starts a new field. maxLen limits the length in characters (0: none).
func TextField(id, value string, maxLen int, onSubmit func(string), onCancel func()) Row {
	return textFieldRow{id, value, maxLen, onSubmit, onCancel}
}

func (textFieldRow) height() int                   { return textFieldH }
func (textFieldRow) segments(*canvas, rect) []rect { return nil }
func (textFieldRow) click(int)                     {}
func (textFieldRow) keepsOpen() bool               { return true }

func (t textFieldRow) box(c *canvas, r rect) rect {
	in := inset(c, r)
	return rect{in.Left, r.Top + c.px(3), in.Right, r.Bottom - c.px(3)}
}

func (t textFieldRow) paint(c *canvas, r rect, _ rowState) {
	box := t.box(c, r)
	c.roundRect(box, c.px(10), colAccent) // a focused field's ring
	ring := max(1, c.px(1))
	c.roundRect(rect{box.Left + ring, box.Top + ring, box.Right - ring, box.Bottom - ring}, c.px(9), colControl)
}

// editField is the native control of the visible TextField.
type editField struct {
	id   string
	hwnd uintptr
	font uintptr
	row  textFieldRow // the latest row with this id: its callbacks
}

var (
	editOldProc  uintptr
	editProcCB   = windows.NewCallback(editProc)
	editBrush    uintptr
	editBrushCol uint32
)

// editProc subclasses the EDIT control for Enter and Esc. Single-line edits
// beep on both, so their WM_CHAR is swallowed.
func editProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmKeyDown:
		if w == vkReturn || w == vkEscape {
			parent := panelPopup // the popup owns the only field
			if parent != nil {
				code := uintptr(1)
				if w == vkEscape {
					code = 2
				}
				procPostMessageW.Call(parent.hwnd, wmFieldKey, code, 0)
			}
			return 0
		}
	case wmChar:
		if w == '\r' || w == 0x1B {
			return 0
		}
	}
	r, _, _ := procCallWindowProcW.Call(editOldProc, hwnd, uintptr(m), w, l)
	return r
}

// syncField creates, moves or removes the EDIT control to match the rows.
// It runs after every layout and scroll.
func (p *Popup) syncField() {
	var (
		row  textFieldRow
		slot = -1
	)
	for i, s := range p.slots {
		if t, ok := s.row.(textFieldRow); ok {
			row, slot = t, i
			break
		}
	}
	if slot < 0 || (p.field != nil && p.field.id != row.id) {
		p.destroyField()
	}
	if slot < 0 {
		return
	}
	c := p.canvas(0)
	if p.field == nil {
		h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("EDIT"))),
			uintptr(unsafe.Pointer(utf16Ptr(row.value))), wsChild|esAutoHScroll,
			0, 0, 0, 0, p.hwnd, 0, moduleHandle(), 0)
		if h == 0 {
			return
		}
		editOldProc, _, _ = procSetWindowLongPtrW.Call(h, gwlpWndProc, editProcCB)
		if row.maxLen > 0 {
			procSendMessageW.Call(h, emSetLimitText, uintptr(row.maxLen), 0)
		}
		p.field = &editField{id: row.id, hwnd: h}
		defer func() {
			procSendMessageW.Call(h, emSetSel, 0, ^uintptr(0)) // all: typing replaces it
			procSetFocus.Call(h)
		}()
	}
	f := p.field
	f.row = row
	if font := p.fonts.h[fontBody]; f.font != font {
		f.font = font
		procSendMessageW.Call(f.hwnd, wmSetFont, font, 1)
		procSendMessageW.Call(f.hwnd, emSetMargins, ecLeftRight, 0)
	}
	// The text sits inside the painted box, vertically centred; hidden while
	// its row is scrolled out of the panel or under the dock.
	box := row.box(c, p.rowRect(slot))
	textH := c.px(20)
	x, y := box.Left+c.px(10), box.Top+(box.h()-textH)/2
	w := box.w() - 2*c.px(10)
	pad := c.px(popupPadL)
	bottom := p.viewBottom() - pad
	if slot >= p.dockStart() {
		bottom = p.height
	}
	if box.Top < pad || box.Bottom > bottom {
		procShowWindow.Call(f.hwnd, swHide)
		return
	}
	procSetWindowPos.Call(f.hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(textH), swpNoZOrder|swpNoActivate|swpShowWindow)
}

func (p *Popup) destroyField() {
	if p.field != nil {
		procDestroyWindow.Call(p.field.hwnd)
		p.field = nil
	}
}

// fieldText returns what is typed in the visible field.
func (p *Popup) fieldText() string {
	if p.field == nil {
		return ""
	}
	n, _, _ := procGetWindowTextLengthW.Call(p.field.hwnd)
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(p.field.hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return windows.UTF16ToString(buf)
}

// FieldText returns the text typed in the TextField with this id, e.g. for
// a Save button next to it. ok is false when no such field is shown.
func (p *Popup) FieldText(id string) (text string, ok bool) {
	if p.field == nil || p.field.id != id {
		return "", false
	}
	return p.fieldText(), true
}

// fieldKey runs the field's callback for Enter (code 1) or Esc (code 2).
func (p *Popup) fieldKey(code uintptr) {
	if p.field == nil {
		return
	}
	row := p.field.row
	switch {
	case code == 1 && row.onSubmit != nil:
		row.onSubmit(p.fieldText()) // may call SetRows
	case code == 2 && row.onCancel != nil:
		row.onCancel()
	}
}

// fieldColors answers WM_CTLCOLOREDIT: the field's text on its box colour.
func fieldColors(hdc uintptr) uintptr {
	if editBrush == 0 || editBrushCol != colControl {
		if editBrush != 0 {
			procDeleteObject.Call(editBrush)
		}
		editBrush, _, _ = procCreateSolidBrush.Call(uintptr(colControl))
		editBrushCol = colControl
	}
	procSetTextColor.Call(hdc, uintptr(colText))
	procSetBkColor.Call(hdc, uintptr(colControl))
	return editBrush
}
