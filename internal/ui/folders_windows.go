package ui

import "strings"

// Rows of the sound library page: folders as drop fields, the import guide
// with its little drag-and-drop picture, and the section caption with a
// link on the home page.

// --- Folder drop field -------------------------------------------------------------

// FolderSpec describes one FolderDrop row.
type FolderSpec struct {
	Name   string
	Detail string // short, right-aligned, e.g. "12 sounds"
	Tip    string // tooltip on the field, e.g. the full path
	// Depth indents subfolders: 0 for a library folder, 1 for its direct
	// subfolders, and so on.
	Depth int
	// Note replaces Detail in the accent colour for a moment, e.g.
	// "Added “Horn”" or "Downloading…".
	Note string
	// Active marks the folder something is being imported into: its icon
	// takes the accent colour.
	Active bool
	// ActionIcon is an optional square icon button on the right, as tall as
	// the field, e.g. a trash can to remove the folder; ActionTip is its
	// tooltip.
	ActionIcon Icon
	ActionTip  string
	OnAction   func()
	// OnClick runs for a click on the field, e.g. to show its sounds.
	OnClick func()
	// Expanded shows the folder open (its sound list follows it).
	Expanded bool
	// OnOpen, if set, adds a small "Show in Explorer" button that takes the
	// place of Detail and Note while the pointer is over the row.
	OnOpen func()
	// OnDrop receives files and links dropped on the field.
	OnDrop func(DropData)
	// DropHint, if set, says what a drop of the dragged things would do,
	// in place of "Drop to add here"; "" keeps that.
	DropHint func(DropData) string
}

type folderDropRow struct{ s FolderSpec }

// FolderDrop is a folder shown as a dashed drop field: things dragged onto
// it go to its OnDrop (not to Popup.OnDrop), and it lights up while they
// are over it.
func FolderDrop(s FolderSpec) Row { return folderDropRow{s} }

const (
	folderIndentL   = 16 // per level of depth
	folderMaxIndent = 4  // deeper levels line up with the fourth
)

func (f folderDropRow) height() int {
	if f.s.Depth > 0 {
		return 36
	}
	return 42
}

// folderOpenLabel is the hover button's label (OnOpen).
const folderOpenLabel = "Show in Explorer"

// geometry returns the dashed field, the action button (empty without an
// action) and the "Show in Explorer" button inside the field's right end
// (empty without OnOpen). The action button is square and exactly as tall
// as the field.
func (f folderDropRow) geometry(c *canvas, r rect) (box, action, open rect) {
	in := inset(c, r)
	box = rect{in.Left + c.px(folderIndentL*min(f.s.Depth, folderMaxIndent)), r.Top + c.px(3), in.Right, r.Bottom - c.px(3)}
	if f.s.ActionIcon != "" {
		action = rect{in.Right - box.h(), box.Top, in.Right, box.Bottom}
		box.Right = action.Left - c.px(6)
	}
	if f.s.OnOpen != nil {
		h := min(c.px(24), box.h()-c.px(8))
		w := c.textWidth(folderOpenLabel, fontCaption) + c.px(8+14+6+10)
		top := box.Top + (box.h()-h)/2
		open = rect{box.Right - c.px(5) - w, top, box.Right - c.px(5), top + h}
	}
	return box, action, open
}

// The field (segment 0) leaves out the "Show in Explorer" button (2):
// segments are hit-tested in order, and the button lies inside the field.
func (f folderDropRow) segments(c *canvas, r rect) []rect {
	box, action, open := f.geometry(c, r)
	switch {
	case f.s.OnClick == nil:
		box = rect{}
	case open.w() > 0:
		box.Right = open.Left
	}
	return []rect{box, action, open}
}

func (f folderDropRow) click(seg int) {
	switch {
	case seg == 0 && f.s.OnClick != nil:
		f.s.OnClick()
	case seg == 1 && f.s.OnAction != nil:
		f.s.OnAction()
	case seg == 2 && f.s.OnOpen != nil:
		f.s.OnOpen()
	}
}

func (folderDropRow) keepsOpen() bool               { return true }
func (f folderDropRow) dropHandler() func(DropData) { return f.s.OnDrop }

func (f folderDropRow) tip(_ *canvas, _ rect, seg int) string {
	switch seg {
	case 0:
		return f.s.Tip
	case 1:
		return f.s.ActionTip
	case 2:
		return "Open the folder in Explorer"
	}
	return ""
}

func (f folderDropRow) paint(c *canvas, r rect, st rowState) {
	box, action, open := f.geometry(c, r)
	// The pointer over the row shows the Explorer button; over the field
	// or that button, the field lights up.
	showOpen := open.w() > 0 && st.hover >= 0 && !st.drop
	border := colControl
	switch {
	case st.drop:
		c.roundRect(box, c.px(10), mix(colBackground, colAccent, 0.16))
		border = colAccent
	case st.press == 0 || st.press == 2:
		c.roundRect(box, c.px(10), colRowPress)
		border = colMuted
	case st.hover == 0 || st.hover == 2:
		c.roundRect(box, c.px(10), colRowHover)
		border = colMuted
	case f.s.Active:
		border = mix(colControl, colAccent, 0.6)
	}
	c.dashedFrame(box, c.px(10), border)

	font := fontBold
	if f.s.Depth > 0 {
		font = fontBody
	}
	icon, iconCol := IconFolder, colMuted
	if f.s.Expanded {
		icon = IconFolderOpen
	}
	switch {
	case st.drop:
		icon, iconCol = IconDownload, colAccent
	case f.s.Active:
		iconCol = colAccent
	}
	pad := c.px(10)
	iconBox := rect{box.Left + pad, box.Top, box.Left + pad + c.px(16), box.Bottom}
	c.icon(icon, iconBox, 16, iconCol)
	detail, detailCol := f.s.Detail, colMuted
	switch {
	case st.drop:
		detail, detailCol = "Drop to add here", colAccent
		if f.s.DropHint != nil && st.dragged != nil {
			if hint := f.s.DropHint(*st.dragged); hint != "" {
				detail = hint
			}
		}
	case f.s.Note != "":
		detail, detailCol = f.s.Note, colAccent
	}
	// A note may be long (a sound's name): it gets up to two thirds.
	maxW := box.w() / 2
	if f.s.Note != "" {
		maxW = box.w() * 2 / 3
	}
	detailW := min(c.textWidth(detail, fontCaption)+c.px(2), maxW)
	nameRight := box.Right - pad - detailW - c.px(8)
	if showOpen {
		nameRight = open.Left - c.px(8)
		bg, fg := colControl, colMuted
		if st.hover == 2 {
			bg, fg = colRowPress, colText
		}
		c.roundRect(open, open.h()/2, bg)
		ic := rect{open.Left + c.px(8), open.Top, open.Left + c.px(8+14), open.Bottom}
		c.icon(IconExternalLink, ic, 14, fg)
		c.text(folderOpenLabel, rect{ic.Right + c.px(6), open.Top, open.Right - c.px(8), open.Bottom}, fontCaption, fg, dtLeft)
	}
	c.text(f.s.Name, rect{iconBox.Right + c.px(8), box.Top, nameRight, box.Bottom}, font, colText, dtLeft)
	if !showOpen {
		c.text(detail, rect{box.Right - pad - detailW, box.Top, box.Right - pad, box.Bottom}, fontCaption, detailCol, dtRight)
	}

	if f.s.ActionIcon != "" {
		paintButton(c, action, "", f.s.ActionIcon, ButtonNormal, false, st.hover == 1, st.press == 1)
	}
}

// --- Import guide ------------------------------------------------------------------

type importGuideRow struct{ text, underline string }

// ImportGuide is a paragraph with a small picture on its right: a
// Myinstants-style sound button, its name being dragged along a dotted
// path onto a folder drop field. The words of underline (a phrase in text)
// are drawn brighter and underlined, like the link they describe.
func ImportGuide(text, underline string) Row { return importGuideRow{text, underline} }

const (
	guideWL   = 118 // the picture's logical size
	guideHL   = 84
	guideGapL = 10
)

func (importGuideRow) height() int                   { return guideHL + 6 }
func (importGuideRow) segments(*canvas, rect) []rect { return nil }
func (importGuideRow) click(int)                     {}
func (importGuideRow) keepsOpen() bool               { return true }

func (g importGuideRow) measure(c *canvas, width int32) int32 {
	textW := width - 2*c.px(padX) - c.px(guideWL+guideGapL)
	_, h := g.layout(c, textW)
	return max(h+c.px(8), c.px(guideHL+6))
}

func (g importGuideRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	pic := rect{in.Right - c.px(guideWL), r.Top + c.px(3), in.Right, r.Top + c.px(3+guideHL)}
	left, top := in.Left, r.Top+c.px(3)
	words, _ := g.layout(c, pic.Left-c.px(guideGapL)-left)
	lineH := c.wrapHeight("Ag", c.px(200), fontBody)
	for _, w := range words {
		col := colMuted
		if w.under {
			col = colText
		}
		box := rect{left + w.x, top + w.y, left + w.x + w.w + c.px(2), top + w.y + lineH}
		c.text(w.text, box, fontBody, col, dtLeft)
		if w.under {
			uw := w.w
			if w.joined {
				uw += w.space // the underline runs on to the next word
			}
			y := box.Bottom - c.px(2)
			c.fill(rect{box.Left, y, box.Left + uw, y + max(1, c.px(1))}, colAccent)
		}
	}
	paintGuide(c, pic)
}

// guideWord is one word of the guide's text, laid out.
type guideWord struct {
	text   string
	x, y   int32 // relative to the text's top left
	w      int32
	space  int32 // the space after it
	under  bool  // part of the underlined phrase
	joined bool  // the next word is too, on the same line
}

// layout wraps the text into width, word by word (GDI's own word wrap
// can't underline part of a paragraph), and returns the words and the
// height they take.
func (g importGuideRow) layout(c *canvas, width int32) ([]guideWord, int32) {
	lineH := c.wrapHeight("Ag", c.px(200), fontBody)
	if lineH == 0 { // no device context to measure with
		return nil, c.wrapHeight(g.text, width, fontBody)
	}
	ulStart, ulEnd := -1, -1
	if i := strings.Index(g.text, g.underline); g.underline != "" && i >= 0 {
		ulStart, ulEnd = i, i+len(g.underline)
	}
	space := c.textWidth(" ", fontBody)
	var words []guideWord
	var x, y int32
	pos := 0
	for _, f := range strings.Fields(g.text) {
		start := pos + strings.Index(g.text[pos:], f)
		pos = start + len(f)
		w := c.textWidth(f, fontBody)
		if x > 0 && x+w > width {
			x, y = 0, y+lineH
		}
		under := ulStart >= 0 && start >= ulStart && start+len(f) <= ulEnd
		if n := len(words); n > 0 && under && words[n-1].under && words[n-1].y == y {
			words[n-1].joined = true
		}
		words = append(words, guideWord{text: f, x: x, y: y, w: w, space: space, under: under})
		x += w + space
	}
	return words, y + lineH
}

// paintGuide draws the drag-and-drop picture in box (guideWL × guideHL
// logical pixels): a Myinstants button with its name below; the name,
// picked up by a hand, travelling along a dotted path; a folder field in
// its "drop here" look below the hand.
func paintGuide(c *canvas, box rect) {
	at := func(x, y float64) (int32, int32) {
		return box.Left + int32(x*c.scale+0.5), box.Top + int32(y*c.scale+0.5)
	}
	c.roundRect(box, c.px(10), colList) // one little scene

	// A Myinstants-style button in the panel's berry red: a dome on its
	// darker base, with a highlight.
	bx, by := at(26, 21)
	c.circle(bx, by+c.px(3), c.px(14), shade(colDanger, -90))
	c.circle(bx, by, c.px(13), colDanger)
	c.circle(bx-c.px(4), by-c.px(4), c.px(3), shade(colDanger, 45))

	// Its name below, underlined like the link it is.
	const label = "Bruh"
	nl, nt := at(6, 40)
	nr, nb := at(46, 54)
	name := rect{nl, nt, nr, nb}
	c.text(label, name, fontCaption, colText, dtCenter)
	w := c.textWidth(label, fontCaption)
	c.fill(rect{name.Left + (name.w()-w)/2, nb - c.px(1), name.Left + (name.w()+w)/2, nb - c.px(1) + max(1, c.px(1))}, colAccent)

	// The path: from the name, up and over to the hand.
	dot := int32(1.6*c.scale + 0.5)
	const dots = 7
	for i := 1; i <= dots; i++ {
		t := float64(i) / (dots + 1)
		x := (1-t)*(1-t)*40 + 2*(1-t)*t*56 + t*t*76
		y := (1-t)*(1-t)*44 + 2*(1-t)*t*6 + t*t*24
		px, py := at(x, y)
		c.circle(px, py, dot, mix(colList, colAccent, 0.3+0.6*t))
	}

	// The folder field, waiting below the hand.
	fl, ft := at(68, 56)
	fr, fb := at(float64(guideWL-6), float64(guideHL-6))
	field := rect{fl, ft, fr, fb}
	c.roundRect(field, c.px(7), mix(colList, colAccent, 0.18))
	c.dashedFrame(field, c.px(7), colAccent)
	c.icon(IconFolder, rect{field.Left + c.px(4), field.Top, field.Left + c.px(18), field.Bottom}, 12, colAccent)
	bar := field.Top + field.h()/2
	c.fill(rect{field.Left + c.px(21), bar - c.px(1), field.Right - c.px(6), bar - c.px(1) + max(2, c.px(2))}, mix(colList, colAccent, 0.5))

	// The name being carried, and the hand holding it.
	gl, gt := at(70, 22)
	gr, gb := at(102, 36)
	ghost := rect{gl, gt, gr, gb}
	c.roundRect(ghost, c.px(6), colTip)
	c.text(label, ghost, fontCaption, colText, dtCenter)
	hx, hy := at(92, 30)
	c.icon(IconHandGrab, rect{hx, hy, hx + c.px(18), hy + c.px(18)}, 16, colText)

	// Two dots down: where it lands.
	for i, y := range []float64{44, 50} {
		px, py := at(86, y)
		c.circle(px, py, dot, mix(colList, colAccent, 0.6+0.3*float64(i)))
	}
}

// --- Section caption with a link ------------------------------------------------------

type sectionLinkRow struct {
	text, link, tipText string
	onClick             func()
}

// SectionLink is a Section caption with a small accent link at its right
// end, e.g. a shortcut to the page where something is done. tip is the
// link's tooltip.
func SectionLink(text, link, tip string, onClick func()) Row {
	return sectionLinkRow{text, link, tip, onClick}
}

func (s sectionLinkRow) tip(_ *canvas, _ rect, seg int) string {
	if seg == 0 {
		return s.tipText
	}
	return ""
}

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

// --- Groups ---------------------------------------------------------------------------

// colGroup is the band behind a group of rows: a little lighter than the
// panel, darker than a hovered row.
var colGroup = rgb(0x26, 0x28, 0x2F)

type groupMarkRow struct{ start bool }

// GroupStart and GroupEnd enclose rows that belong together, such as a
// folder and the import into it: the panel draws one rounded band behind
// them. The markers are a few pixels of padding themselves.
func GroupStart() Row { return groupMarkRow{true} }
func GroupEnd() Row   { return groupMarkRow{false} }

func (groupMarkRow) height() int                   { return 4 }
func (groupMarkRow) segments(*canvas, rect) []rect { return nil }
func (groupMarkRow) click(int)                     {}
func (groupMarkRow) keepsOpen() bool               { return true }
func (groupMarkRow) paint(*canvas, rect, rowState) {}

// paintGroups draws the bands of the groups among n rows; row(i) and
// rowRect(i) give each row and where it is. Called before the rows paint.
func paintGroups(c *canvas, n int, row func(int) Row, rowRect func(int) rect) {
	start := -1
	for i := 0; i < n; i++ {
		g, ok := row(i).(groupMarkRow)
		switch {
		case !ok:
		case g.start:
			start = i
		case start >= 0:
			top, bottom := rowRect(start), rowRect(i)
			band := rect{top.Left + c.px(6), top.Top, top.Right - c.px(6), bottom.Bottom}
			c.roundRect(band, c.px(14), colGroup)
			start = -1
		}
	}
}
