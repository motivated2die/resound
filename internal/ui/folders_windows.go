package ui

// Rows of the sound library page: folders as drop fields, and the section
// caption with a link on the home page.

// --- Folder drop field -------------------------------------------------------------

// FolderSpec describes one FolderDrop row.
type FolderSpec struct {
	Name   string
	Detail string // short, right-aligned, e.g. "12 sounds"
	Tip    string // tooltip, e.g. the full path
	// Sub indents the row: a subfolder below its library folder.
	Sub bool
	// Action is an optional small button on the right, e.g. "Remove".
	Action   string
	OnAction func()
	// OnClick runs for a click on the field, e.g. to open the folder.
	OnClick func()
	// OnDrop receives files and links dropped on the field.
	OnDrop func(DropData)
}

type folderDropRow struct{ s FolderSpec }

// FolderDrop is a folder shown as a dashed drop field: things dragged onto
// it go to its OnDrop (not to Popup.OnDrop), and it lights up while they
// are over it.
func FolderDrop(s FolderSpec) Row { return folderDropRow{s} }

func (f folderDropRow) height() int {
	if f.s.Sub {
		return 36
	}
	return 42
}

// geometry returns the dashed field and the action button (empty without
// an action).
func (f folderDropRow) geometry(c *canvas, r rect) (box, action rect) {
	in := inset(c, r)
	left := in.Left
	if f.s.Sub {
		left += c.px(18)
	}
	right := in.Right
	if f.s.Action != "" {
		h := c.px(26)
		top := r.Top + (r.h()-h)/2
		action = rect{in.Right - c.px(72), top, in.Right, top + h}
		right = action.Left - c.px(8)
	}
	return rect{left, r.Top + c.px(3), right, r.Bottom - c.px(3)}, action
}

func (f folderDropRow) segments(c *canvas, r rect) []rect {
	box, action := f.geometry(c, r)
	if f.s.OnClick == nil {
		box = rect{}
	}
	return []rect{box, action}
}

func (f folderDropRow) click(seg int) {
	switch {
	case seg == 0 && f.s.OnClick != nil:
		f.s.OnClick()
	case seg == 1 && f.s.OnAction != nil:
		f.s.OnAction()
	}
}

func (folderDropRow) keepsOpen() bool               { return true }
func (f folderDropRow) dropHandler() func(DropData) { return f.s.OnDrop }
func (f folderDropRow) tip(_ *canvas, _ rect, seg int) string {
	if seg == 0 {
		return f.s.Tip
	}
	return ""
}

func (f folderDropRow) paint(c *canvas, r rect, st rowState) {
	box, action := f.geometry(c, r)
	border := colControl
	switch {
	case st.drop:
		c.roundRect(box, c.px(10), mix(colBackground, colAccent, 0.16))
		border = colAccent
	case st.press == 0:
		c.roundRect(box, c.px(10), colRowPress)
		border = colMuted
	case st.hover == 0:
		c.roundRect(box, c.px(10), colRowHover)
		border = colMuted
	}
	c.dashedFrame(box, c.px(10), border)

	iconCol, font := colMuted, fontBold
	if f.s.Sub {
		font = fontBody
	}
	if st.drop {
		iconCol = colAccent
	}
	pad := c.px(10)
	icon := IconFolder
	if st.drop {
		icon = IconDownload
	}
	iconBox := rect{box.Left + pad, box.Top, box.Left + pad + c.px(16), box.Bottom}
	c.icon(icon, iconBox, 16, iconCol)
	detail, detailCol := f.s.Detail, colMuted
	if st.drop {
		detail, detailCol = "Drop to add here", colAccent
	}
	detailW := min(c.textWidth(detail, fontCaption)+c.px(2), box.w()/2)
	nameRight := box.Right - pad - detailW - c.px(8)
	c.text(f.s.Name, rect{iconBox.Right + c.px(8), box.Top, nameRight, box.Bottom}, font, colText, dtLeft)
	c.text(detail, rect{box.Right - pad - detailW, box.Top, box.Right - pad, box.Bottom}, fontCaption, detailCol, dtRight)

	if f.s.Action != "" {
		bg := colControl
		if st.press == 1 {
			bg = shade(bg, -18)
		} else if st.hover == 1 {
			bg = shade(bg, 14)
		}
		c.roundRect(action, c.px(8), bg)
		c.text(f.s.Action, action, fontCaption, colText, dtCenter)
	}
}

// --- Section caption with a link ------------------------------------------------------

type sectionLinkRow struct {
	text, link string
	onClick    func()
}

// SectionLink is a Section caption with a small accent link at its right
// end, e.g. a shortcut to the page where something is done.
func SectionLink(text, link string, onClick func()) Row { return sectionLinkRow{text, link, onClick} }

func (sectionLinkRow) height() int { return 28 }

func (s sectionLinkRow) linkBox(c *canvas, r rect) rect {
	in := inset(c, r)
	w := c.textWidth(s.link, fontCaption) + c.px(4)
	return rect{in.Right - w, r.Top + c.px(8), in.Right, r.Bottom}
}

func (s sectionLinkRow) segments(c *canvas, r rect) []rect {
	if s.onClick == nil {
		return nil
	}
	return []rect{s.linkBox(c, r)}
}

func (s sectionLinkRow) click(int) {
	if s.onClick != nil {
		s.onClick()
	}
}
func (sectionLinkRow) keepsOpen() bool { return true }

func (s sectionLinkRow) paint(c *canvas, r rect, st rowState) {
	in := inset(c, r)
	in.Top += c.px(8)
	link := s.linkBox(c, r)
	c.text(upper(s.text), rect{in.Left, in.Top, link.Left - c.px(8), in.Bottom}, fontCaption, colCaption, dtLeft)
	col := colAccent
	switch {
	case st.press == 0:
		col = shade(colAccent, -30)
	case st.hover == 0:
		col = shade(colAccent, 30)
	}
	c.text(s.link, link, fontCaption, col, dtRight)
	if st.hover == 0 { // underline while hovered, like a link
		w := c.textWidth(s.link, fontCaption)
		y := link.Top + link.h()/2 + c.px(7)
		c.fill(rect{link.Right - w, y, link.Right, y + max(1, c.px(1))}, col)
	}
}
