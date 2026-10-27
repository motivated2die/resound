package ui

// The dock: rows pinned at the bottom of the panel, over the page, such as
// the files open in the editor on the library page. The page's rows scroll
// above it and can be scrolled all the way out from under it.

const (
	dockMarginL = 6  // the dock's card from the panel's edges and the dock's top
	dockPadL    = 10 // the docked rows from the dock's top and bottom edge
	dockRadiusL = 14 // the card's corners, like a group's band
)

type dockRow struct{ rows []Row }

// Docked pins rows to the bottom of the panel, on a card framed in the
// accent: they stay in view while the page's rows scroll above them (the
// page scrolls further by the dock's height, so its last rows come out
// from under it). It may be anywhere in a page's rows; a page has at most
// one. Without rows, nothing is docked.
func Docked(rows ...Row) Row { return dockRow{rows} }

// The popup takes the rows out of a dockRow (Popup.layout); the row itself
// is never placed.
func (dockRow) height() int                   { return 0 }
func (dockRow) segments(*canvas, rect) []rect { return nil }
func (dockRow) click(int)                     {}
func (dockRow) keepsOpen() bool               { return true }
func (dockRow) paint(*canvas, rect, rowState) {}

// paintDock draws the dock's area, area, over the rows scrolled under it:
// the panel's background with a divider along its top, and the card, framed
// like a folder something is being done in.
func paintDock(c *canvas, area rect) {
	c.fill(area, colBackground)
	c.fill(rect{area.Left, area.Top, area.Right, area.Top + max(1, c.px(1))}, colDivider)
	m := c.px(dockMarginL)
	card := rect{area.Left + m, area.Top + m, area.Right - m, area.Bottom - m}
	c.roundRect(card, c.px(dockRadiusL), colGroup)
	c.outline(card, c.px(dockRadiusL), mix(colControl, colAccent, 0.6))
}

// --- Task item -------------------------------------------------------------------

// TaskSpec describes one TaskItem row.
type TaskSpec struct {
	Icon   Icon   // on the left, in the accent colour
	Name   string // e.g. the file's name
	Detail string // the line below, e.g. "Cutting clips into “Anime”"
	Tip    string // tooltip on the name and detail, e.g. the full path
	// Busy draws the detail in the accent colour (under way); it is muted
	// otherwise (starting).
	Busy  bool
	Tools []Tool // square buttons on the right, left to right
}

type taskItemRow struct{ s TaskSpec }

// TaskItem is a file something is being done with elsewhere, such as one
// open in the editor: an icon, its name with a line about it below, and
// square icon buttons for it on the right (each with its tooltip).
func TaskItem(s TaskSpec) Row { return taskItemRow{s} }

const taskToolL = 30 // a tool button's size

func (taskItemRow) height() int     { return 46 }
func (taskItemRow) keepsOpen() bool { return true }

// geometry returns the icon's box, the text's box and the tools' squares,
// left to right.
func (t taskItemRow) geometry(c *canvas, r rect) (icon, text rect, tools []rect) {
	in := inset(c, r)
	size := c.px(taskToolL)
	top := r.Top + (r.h()-size)/2
	right := in.Right
	for range t.s.Tools {
		tools = append(tools, rect{right - size, top, right, top + size})
		right -= size + c.px(6)
	}
	// Built right to left: the first tool sits leftmost.
	for i, j := 0, len(tools)-1; i < j; i, j = i+1, j-1 {
		tools[i], tools[j] = tools[j], tools[i]
	}
	icon = rect{in.Left + c.px(2), r.Top, in.Left + c.px(2+18), r.Bottom}
	text = rect{icon.Right + c.px(10), r.Top, in.Right, r.Bottom}
	if len(tools) > 0 {
		text.Right = tools[0].Left - c.px(8)
	}
	return icon, text, tools
}

// Segment 0 is the text (its tooltip; empty without one), then one per
// tool.
func (t taskItemRow) segments(c *canvas, r rect) []rect {
	_, text, tools := t.geometry(c, r)
	if t.s.Tip == "" {
		text = rect{}
	}
	return append([]rect{text}, tools...)
}

func (t taskItemRow) click(seg int) {
	if seg >= 1 && seg <= len(t.s.Tools) && t.s.Tools[seg-1].OnClick != nil {
		t.s.Tools[seg-1].OnClick()
	}
}

func (t taskItemRow) tip(_ *canvas, _ rect, seg int) string {
	switch {
	case seg == 0:
		return t.s.Tip
	case seg <= len(t.s.Tools):
		return t.s.Tools[seg-1].Tip
	}
	return ""
}

func (t taskItemRow) paint(c *canvas, r rect, st rowState) {
	icon, text, tools := t.geometry(c, r)
	if t.s.Icon != "" {
		c.icon(t.s.Icon, icon, 16, colAccent)
	}
	mid := r.Top + r.h()/2
	c.text(t.s.Name, rect{text.Left, r.Top + c.px(4), text.Right, mid + c.px(1)}, fontBold, colText, dtLeft)
	detailCol := colMuted
	if t.s.Busy {
		detailCol = colAccent
	}
	c.text(t.s.Detail, rect{text.Left, mid + c.px(1), text.Right, r.Bottom - c.px(4)}, fontCaption, detailCol, dtLeft)
	for i, box := range tools {
		tool := t.s.Tools[i]
		hover, press := st.hover == i+1, st.press == i+1
		style := ButtonNormal
		if tool.Danger && (hover || press) {
			style = ButtonDanger
		}
		paintButton(c, box, "", tool.Icon, style, false, hover, press)
	}
}
