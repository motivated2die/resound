package ui

// Row is one line of the popup panel. Rows are cheap, immutable values:
// the app rebuilds the whole list whenever state changes (see Popup.SetRows).
type Row interface {
	// height is the row height in logical (96-DPI) pixels.
	height() int
	// segments returns the clickable regions inside r (device pixels).
	// A nil result means the row is not interactive.
	segments(c *canvas, r rect) []rect
	paint(c *canvas, r rect, st rowState)
	// click runs on the UI thread after the popup lock is released.
	click(seg int)
	// keepsOpen reports whether clicking leaves the popup open.
	keepsOpen() bool
}

type rowState struct {
	hover, press int       // segment index or -1
	open         int       // segment whose dropdown list is open, or -1
	drop         bool      // something dragged is over this row (a drop field)
	dragged      *DropData // with drop: what is being dragged
}

// Tone colors informational text.
type Tone int

const (
	ToneNormal Tone = iota
	ToneMuted
	ToneGood
	ToneWarning
	ToneBad
)

func (t Tone) color() uint32 {
	switch t {
	case ToneMuted:
		return colMuted
	case ToneGood:
		return colAccent
	case ToneWarning:
		return colWarning
	case ToneBad:
		return colDanger
	}
	return colText
}

const padX = 14 // logical horizontal padding of the panel

func inset(c *canvas, r rect) rect {
	return rect{r.Left + c.px(padX), r.Top, r.Right - c.px(padX), r.Bottom}
}

// --- Title -----------------------------------------------------------------

type titleRow struct{ title, subtitle string }

// Title is the panel header.
func Title(title, subtitle string) Row { return titleRow{title, subtitle} }

func (titleRow) height() int                   { return 50 }
func (titleRow) segments(*canvas, rect) []rect { return nil }
func (titleRow) click(int)                     {}
func (titleRow) keepsOpen() bool               { return true }
func (t titleRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	mid := r.Top + r.h()/2
	c.text(t.title, rect{in.Left, r.Top + c.px(6), in.Right, mid + c.px(2)}, fontTitle, colText, dtLeft)
	c.text(t.subtitle, rect{in.Left, mid + c.px(2), in.Right, r.Bottom - c.px(4)}, fontCaption, colCaption, dtLeft)
}

// --- Section caption -------------------------------------------------------

type sectionRow struct{ text string }

// Section is an upper-case caption that starts a group of rows.
func Section(text string) Row { return sectionRow{text} }

func (sectionRow) height() int                   { return 28 }
func (sectionRow) segments(*canvas, rect) []rect { return nil }
func (sectionRow) click(int)                     {}
func (sectionRow) keepsOpen() bool               { return true }
func (s sectionRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	in.Top += c.px(8)
	c.text(upper(s.text), in, fontCaption, colCaption, dtLeft)
}

// --- Text ------------------------------------------------------------------

type textRow struct {
	label, value string
	tone         Tone
}

// Text shows a label and, optionally, a right-aligned value.
func Text(label, value string, tone Tone) Row { return textRow{label, value, tone} }

func (textRow) height() int                   { return 22 }
func (textRow) segments(*canvas, rect) []rect { return nil }
func (textRow) click(int)                     {}
func (textRow) keepsOpen() bool               { return true }
func (t textRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	if t.value == "" {
		c.text(t.label, in, fontBody, t.tone.color(), dtLeft)
		return
	}
	split := in.Left + in.w()*42/100
	c.text(t.label, rect{in.Left, in.Top, split, in.Bottom}, fontBody, colMuted, dtLeft)
	c.text(t.value, rect{split + c.px(8), in.Top, in.Right, in.Bottom}, fontBody, t.tone.color(), dtRight)
}

// --- Button ----------------------------------------------------------------

// ButtonStyle selects a button's look.
type ButtonStyle int

const (
	ButtonNormal ButtonStyle = iota
	ButtonPrimary
	ButtonDanger
)

type buttonRow struct {
	label    string
	style    ButtonStyle
	disabled bool
	stayOpen bool
	onClick  func()
}

// Button is a full-width push button. Clicking it keeps the popup open
// unless CloseOnClick is applied.
func Button(label string, style ButtonStyle, disabled bool, onClick func()) Row {
	return buttonRow{label: label, style: style, disabled: disabled, stayOpen: true, onClick: onClick}
}

// ClosingButton is a button that closes the popup when clicked.
func ClosingButton(label string, style ButtonStyle, onClick func()) Row {
	return buttonRow{label: label, style: style, onClick: onClick}
}

func (buttonRow) height() int { return 38 }
func (b buttonRow) segments(c *canvas, r rect) []rect {
	if b.disabled {
		return nil
	}
	return []rect{b.box(c, r)}
}
func (b buttonRow) box(c *canvas, r rect) rect {
	in := inset(c, r)
	return rect{in.Left, r.Top + c.px(3), in.Right, r.Bottom - c.px(3)}
}
func (b buttonRow) click(int) {
	if b.onClick != nil {
		b.onClick()
	}
}
func (b buttonRow) keepsOpen() bool { return b.stayOpen }
func (b buttonRow) paint(c *canvas, r rect, st rowState) {
	paintButton(c, b.box(c, r), b.label, "", b.style, b.disabled, st.hover == 0, st.press == 0)
}

const buttonIconL = 16 // logical icon size inside buttons

// paintButton draws a push button with an optional icon before the label
// (or alone when the label is empty); shared by Button, Buttons, NavTool.
func paintButton(c *canvas, box rect, label string, icon Icon, style ButtonStyle, disabled, hover, press bool) {
	bg, fg := buttonColors(style, disabled, hover, press)
	c.roundRect(box, c.px(10), bg)
	paintButtonFace(c, box, label, icon, fg)
}

// buttonColors are a button's background and text colour.
func buttonColors(style ButtonStyle, disabled, hover, press bool) (bg, fg uint32) {
	bg, fg = colControl, colText
	switch {
	case disabled:
		bg, fg = colRowHover, colMuted
	case style == ButtonPrimary:
		bg, fg = colAccent, colAccentText
	case style == ButtonDanger:
		fg = colDanger
	}
	if !disabled {
		if press {
			bg = shade(bg, -18)
		} else if hover {
			bg = shade(bg, 14)
		}
	}
	return bg, fg
}

// paintButtonFace draws a button's icon and label centred in box.
func paintButtonFace(c *canvas, box rect, label string, icon Icon, fg uint32) {
	switch {
	case icon == "":
		c.text(label, box, fontBold, fg, dtCenter)
	case label == "":
		c.icon(icon, box, buttonIconL, fg)
	default:
		iw, gap := c.px(buttonIconL), c.px(7)
		tw := min(c.textWidth(label, fontBold)+c.px(2), box.w()-iw-gap-2*c.px(10))
		left := box.Left + (box.w()-iw-gap-tw)/2
		c.icon(icon, rect{left, box.Top, left + iw, box.Bottom}, buttonIconL, fg)
		c.text(label, rect{left + iw + gap, box.Top, left + iw + gap + tw, box.Bottom}, fontBold, fg, dtLeft)
	}
}

// --- Radio -----------------------------------------------------------------

type radioRow struct {
	label, detail string
	selected      bool
	onClick       func()
}

// Radio is one option of a single-choice list.
func Radio(label, detail string, selected bool, onClick func()) Row {
	return radioRow{label, detail, selected, onClick}
}

func (radioRow) height() int { return 32 }
func (radioRow) segments(c *canvas, r rect) []rect {
	return []rect{{r.Left + c.px(6), r.Top + c.px(1), r.Right - c.px(6), r.Bottom - c.px(1)}}
}
func (o radioRow) click(int) {
	if o.onClick != nil {
		o.onClick()
	}
}
func (radioRow) keepsOpen() bool { return true }

// tip returns the full label when it is cut off.
func (o radioRow) tip(c *canvas, r rect, _ int) string {
	in := inset(c, r)
	right := in.Right
	if o.detail != "" {
		right -= c.px(70)
	}
	if c.textWidth(o.label, fontBody) <= right-(in.Left+c.px(21)) {
		return ""
	}
	return o.label
}
func (o radioRow) paint(c *canvas, r rect, st rowState) {
	bg := colBackground
	if st.press == 0 {
		bg = colRowPress
	} else if st.hover == 0 {
		bg = colRowHover
	}
	if bg != colBackground {
		c.roundRect(o.segments(c, r)[0], c.px(8), bg)
	}
	in := inset(c, r)
	cx, cy := in.Left+c.px(7), r.Top+r.h()/2
	ring := colMuted
	if o.selected {
		ring = colAccent
	}
	c.circle(cx, cy, c.px(7), ring)
	c.circle(cx, cy, c.px(5), bg)
	if o.selected {
		c.circle(cx, cy, c.px(3), colAccent)
	}
	textLeft := cx + c.px(14)
	if o.detail == "" {
		c.text(o.label, rect{textLeft, in.Top, in.Right, in.Bottom}, fontBody, colText, dtLeft)
		return
	}
	split := in.Right - c.px(64)
	c.text(o.label, rect{textLeft, in.Top, split - c.px(6), in.Bottom}, fontBody, colText, dtLeft)
	c.text(o.detail, rect{split, in.Top, in.Right, in.Bottom}, fontCaption, colMuted, dtRight)
}

// --- Stepper ---------------------------------------------------------------

type stepperRow struct {
	label, value string
	onDec, onInc func()
}

// Stepper shows a value with − / + buttons.
func Stepper(label, value string, onDec, onInc func()) Row {
	return stepperRow{label, value, onDec, onInc}
}

func (stepperRow) height() int { return 36 }
func (s stepperRow) segments(c *canvas, r rect) []rect {
	in := inset(c, r)
	size := c.px(28)
	top := r.Top + (r.h()-size)/2
	plus := rect{in.Right - size, top, in.Right, top + size}
	minus := rect{plus.Left - c.px(64) - size, top, plus.Left - c.px(64), top + size}
	return []rect{minus, plus}
}
func (s stepperRow) click(seg int) {
	if seg == 0 && s.onDec != nil {
		s.onDec()
	} else if seg == 1 && s.onInc != nil {
		s.onInc()
	}
}
func (stepperRow) keepsOpen() bool { return true }
func (s stepperRow) paint(c *canvas, r rect, st rowState) {
	in := inset(c, r)
	segs := s.segments(c, r)
	c.text(s.label, rect{in.Left, in.Top, segs[0].Left - c.px(8), in.Bottom}, fontBody, colText, dtLeft)
	for i, glyph := range []string{"−", "+"} {
		bg := colControl
		if st.press == i {
			bg = shade(bg, -18)
		} else if st.hover == i {
			bg = shade(bg, 14)
		}
		c.roundRect(segs[i], c.px(8), bg)
		c.text(glyph, segs[i], fontBold, colText, dtCenter)
	}
	c.text(s.value, rect{segs[0].Right, in.Top, segs[1].Left, in.Bottom}, fontBold, colText, dtCenter)
}

// --- Separator & spacer ----------------------------------------------------

type separatorRow struct{}

// Separator is a thin divider line.
func Separator() Row { return separatorRow{} }

func (separatorRow) height() int                   { return 13 }
func (separatorRow) segments(*canvas, rect) []rect { return nil }
func (separatorRow) click(int)                     {}
func (separatorRow) keepsOpen() bool               { return true }
func (separatorRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	y := r.Top + r.h()/2
	c.fill(rect{in.Left, y, in.Right, y + max(1, c.px(1))}, colDivider)
}

type spacerRow struct{ h int }

// Spacer is empty vertical space.
func Spacer(h int) Row { return spacerRow{h} }

func (s spacerRow) height() int                 { return s.h }
func (spacerRow) segments(*canvas, rect) []rect { return nil }
func (spacerRow) click(int)                     {}
func (spacerRow) keepsOpen() bool               { return true }
func (spacerRow) paint(*canvas, rect, rowState) {}

// --- helpers ---------------------------------------------------------------

// shade brightens (positive) or darkens (negative) a COLORREF.
func shade(c uint32, d int) uint32 {
	ch := func(v uint32) uint8 { return uint8(min(255, max(0, int(v)+d))) }
	return rgb(ch(c&0xFF), ch(c>>8&0xFF), ch(c>>16&0xFF))
}

func upper(s string) string {
	b := []rune(s)
	for i, r := range b {
		if r >= 'a' && r <= 'z' {
			b[i] = r - 32
		}
	}
	return string(b)
}

// --- Page header with back button -------------------------------------------

type headerRow struct {
	title  string
	onBack func()
}

// Header is the top row of a sub-page: a back button and a title.
func Header(title string, onBack func()) Row { return headerRow{title, onBack} }

func (headerRow) height() int                         { return 46 }
func (h headerRow) segments(c *canvas, r rect) []rect { return []rect{backButton(c, r)} }
func (h headerRow) click(int) {
	if h.onBack != nil {
		h.onBack()
	}
}
func (headerRow) keepsOpen() bool { return true }
func (h headerRow) paint(c *canvas, r rect, st rowState) {
	back := h.segments(c, r)[0]
	paintBack(c, back, st)
	in := inset(c, r)
	c.text(h.title, headerTitle(c, r, back.Right+c.px(8), in.Right), fontTitle, colText, dtLeft)
}

// --- Page header with back button and a tool button -------------------------------

type headerToolRow struct {
	title   string
	onBack  func()
	icon    Icon
	tipText string
	onTool  func()
}

// HeaderTool is a Header with a square icon button at its right end, such
// as a refresh button. tip is its tooltip; a nil onTool shows it disabled.
func HeaderTool(title string, onBack func(), icon Icon, tip string, onTool func()) Row {
	return headerToolRow{title, onBack, icon, tip, onTool}
}

func (headerToolRow) height() int { return 46 }

func (h headerToolRow) tool(c *canvas, r rect) rect {
	back := backButton(c, r)
	in := inset(c, r)
	return rect{in.Right - back.w(), back.Top, in.Right, back.Bottom}
}

func (h headerToolRow) segments(c *canvas, r rect) []rect {
	tool := h.tool(c, r)
	if h.onTool == nil {
		tool = rect{}
	}
	return []rect{backButton(c, r), tool}
}

func (h headerToolRow) click(seg int) {
	switch {
	case seg == 0 && h.onBack != nil:
		h.onBack()
	case seg == 1 && h.onTool != nil:
		h.onTool()
	}
}
func (headerToolRow) keepsOpen() bool { return true }

func (h headerToolRow) tip(_ *canvas, _ rect, seg int) string {
	if seg == 1 {
		return h.tipText
	}
	return ""
}

func (h headerToolRow) paint(c *canvas, r rect, st rowState) {
	back, tool := backButton(c, r), h.tool(c, r)
	paintBack(c, back, st)
	switch {
	case h.onTool == nil:
	case st.press == 1:
		c.roundRect(tool, c.px(8), colRowPress)
	case st.hover == 1:
		c.roundRect(tool, c.px(8), colRowHover)
	}
	col := colText
	if h.onTool == nil {
		col = colMuted
	}
	c.icon(h.icon, tool, 18, col)
	c.text(h.title, headerTitle(c, r, back.Right+c.px(8), tool.Left-c.px(8)), fontTitle, colText, dtLeft)
}

// backButton is the square back button at the left of a header row; its
// edge lines up with the buttons and tiles below.
func backButton(c *canvas, r rect) rect {
	in := inset(c, r)
	size := c.px(30)
	top := r.Top + (r.h()-size)/2
	return rect{in.Left, top, in.Left + size, top + size}
}

// headerTitle is where a header's title goes. GDI centres the whole line
// box, descender space included, which puts the letters a little below the
// back chevron; lifting the box re-centres them optically.
func headerTitle(c *canvas, r rect, left, right int32) rect {
	return rect{left, r.Top, right, r.Bottom - c.px(4)}
}

func paintBack(c *canvas, back rect, st rowState) {
	if st.press == 0 {
		c.roundRect(back, c.px(8), colRowPress)
	} else if st.hover == 0 {
		c.roundRect(back, c.px(8), colRowHover)
	}
	c.icon(IconChevronLeft, back, 20, colText)
}

// --- Navigation row ----------------------------------------------------------

type navRow struct {
	label, detail string
	tone          Tone
	onClick       func()
}

// Nav opens a sub-page. detail is a short status shown on the right.
func Nav(label, detail string, tone Tone, onClick func()) Row {
	return navRow{label, detail, tone, onClick}
}

func (navRow) height() int { return 38 }

// navBox is the nav's tile: as wide as buttons, so it doesn't stand out.
func navBox(c *canvas, r rect) rect {
	in := inset(c, r)
	return rect{in.Left, r.Top + c.px(2), in.Right, r.Bottom - c.px(2)}
}

func (n navRow) segments(c *canvas, r rect) []rect { return []rect{navBox(c, r)} }
func (n navRow) click(int) {
	if n.onClick != nil {
		n.onClick()
	}
}
func (navRow) keepsOpen() bool { return true }
func (n navRow) paint(c *canvas, r rect, st rowState) {
	n.paintIn(c, navBox(c, r), st.hover == 0, st.press == 0)
}

func (n navRow) paintIn(c *canvas, box rect, hover, press bool) {
	bg := colRowHover
	if press {
		bg = colRowPress
	} else if hover {
		bg = shade(colRowHover, 10)
	}
	c.roundRect(box, c.px(10), bg)
	pad := c.px(12)
	chevron := rect{box.Right - pad - c.px(16), box.Top, box.Right - pad, box.Bottom}
	c.icon(IconChevronRight, chevron, 16, colMuted)
	split := box.Left + box.w()*45/100
	c.text(n.label, rect{box.Left + pad, box.Top, split, box.Bottom}, fontBold, colText, dtLeft)
	c.text(n.detail, rect{split, box.Top, chevron.Left - c.px(6), box.Bottom}, fontBody, n.tone.color(), dtRight)
}

// --- Toggle ------------------------------------------------------------------

type toggleRow struct {
	label   string
	on      bool
	onClick func()
}

// Toggle is a labelled on/off switch.
func Toggle(label string, on bool, onClick func()) Row { return toggleRow{label, on, onClick} }

func (toggleRow) height() int { return 36 }
func (t toggleRow) segments(c *canvas, r rect) []rect {
	return []rect{{r.Left + c.px(6), r.Top + c.px(1), r.Right - c.px(6), r.Bottom - c.px(1)}}
}
func (t toggleRow) click(int) {
	if t.onClick != nil {
		t.onClick()
	}
}
func (toggleRow) keepsOpen() bool { return true }
func (t toggleRow) paint(c *canvas, r rect, st rowState) {
	if st.press == 0 {
		c.roundRect(t.segments(c, r)[0], c.px(8), colRowPress)
	} else if st.hover == 0 {
		c.roundRect(t.segments(c, r)[0], c.px(8), colRowHover)
	}
	in := inset(c, r)
	w, h := c.px(36), c.px(20)
	track := rect{in.Right - w, r.Top + (r.h()-h)/2, in.Right, r.Top + (r.h()-h)/2 + h}
	trackCol, knobCol := colControl, colMuted
	if t.on {
		trackCol, knobCol = colAccent, colAccentText
	}
	c.roundRect(track, h, trackCol)
	knobR := h/2 - c.px(3)
	cx := track.Left + h/2
	if t.on {
		cx = track.Right - h/2
	}
	c.circle(cx, track.Top+h/2, knobR, knobCol)
	c.text(t.label, rect{in.Left, r.Top, track.Left - c.px(8), r.Bottom}, fontBody, colText, dtLeft)
}

// --- QR code -----------------------------------------------------------------

type qrRow struct {
	modules [][]bool // true = dark, quiet zone included
}

// QRCode draws a scannable QR code (black on white, centred).
func QRCode(modules [][]bool) Row { return qrRow{modules} }

const qrSizeL = 196 // logical size of the white square

func (qrRow) height() int                   { return qrSizeL + 12 }
func (qrRow) segments(*canvas, rect) []rect { return nil }
func (qrRow) click(int)                     {}
func (qrRow) keepsOpen() bool               { return true }
func (q qrRow) paint(c *canvas, r rect, _ rowState) {
	n := int32(len(q.modules))
	if n == 0 {
		return
	}
	cell := max(1, c.px(qrSizeL)/n) // whole device pixels per module keep it crisp
	size := cell * n
	left := r.Left + (r.w()-size)/2
	top := r.Top + (r.h()-size)/2
	c.roundRect(rect{left - c.px(6), top - c.px(6), left + size + c.px(6), top + size + c.px(6)}, c.px(10), rgb(0xFF, 0xFF, 0xFF))
	for y, line := range q.modules {
		for x, dark := range line {
			if dark {
				x0, y0 := left+int32(x)*cell, top+int32(y)*cell
				c.fill(rect{x0, y0, x0 + cell, y0 + cell}, rgb(0, 0, 0))
			}
		}
	}
}

// --- List item with an action ------------------------------------------------

type itemRow struct {
	label, detail, action string
	onAction              func()
}

// Item is a two-line entry (label, muted detail) with a small action button.
func Item(label, detail, action string, onAction func()) Row {
	return itemRow{label, detail, action, onAction}
}

func (itemRow) height() int { return 44 }
func (it itemRow) segments(c *canvas, r rect) []rect {
	in := inset(c, r)
	h := c.px(26)
	top := r.Top + (r.h()-h)/2
	return []rect{{in.Right - c.px(72), top, in.Right, top + h}}
}
func (it itemRow) click(int) {
	if it.onAction != nil {
		it.onAction()
	}
}
func (itemRow) keepsOpen() bool { return true }
func (it itemRow) paint(c *canvas, r rect, st rowState) {
	in := inset(c, r)
	btn := it.segments(c, r)[0]
	mid := r.Top + r.h()/2
	c.text(it.label, rect{in.Left, r.Top + c.px(4), btn.Left - c.px(8), mid + c.px(1)}, fontBold, colText, dtLeft)
	c.text(it.detail, rect{in.Left, mid + c.px(1), btn.Left - c.px(8), r.Bottom - c.px(4)}, fontCaption, colMuted, dtLeft)
	bg := colControl
	if st.press == 0 {
		bg = shade(bg, -18)
	} else if st.hover == 0 {
		bg = shade(bg, 14)
	}
	c.roundRect(btn, c.px(8), bg)
	c.text(it.action, btn, fontCaption, colText, dtCenter)
}

// --- Flex --------------------------------------------------------------------

type flexRow struct{}

// Flex is empty space that takes up what is left when a page is shorter
// than the panel's minimum height, keeping the rows after it (a footer) at
// the bottom. Only the first Flex of a page grows.
func Flex() Row { return flexRow{} }

func (flexRow) height() int                   { return 0 }
func (flexRow) segments(*canvas, rect) []rect { return nil }
func (flexRow) click(int)                     {}
func (flexRow) keepsOpen() bool               { return true }
func (flexRow) paint(*canvas, rect, rowState) {}
