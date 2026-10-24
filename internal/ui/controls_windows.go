package ui

// Composite controls: sliders, button groups, wrapped paragraphs and the
// guided-setup header. Like all rows they are immutable values rebuilt on
// every render; sliders keep their drag attached by id (see dragger).

import "math"

// --- Slider ------------------------------------------------------------------

type sliderRow struct {
	id, label, value string
	pos, max, step   int
	onChange         func(value int, done bool)
}

// Slider is a labelled value with a draggable track between − and +
// buttons. The buttons move to the previous/next multiple of step; clicking
// or dragging the track sets the value directly. onChange gets done=false
// while the mouse moves and done=true for button clicks and on release, so
// callers can apply every change but save only once. Values run from 0 to
// maxValue. id must be unique on the page and stable across re-renders.
func Slider(id, label, valueText string, value, maxValue, step int, onChange func(value int, done bool)) Row {
	return sliderRow{id: id, label: label, value: valueText, pos: max(0, min(value, maxValue)),
		max: max(1, maxValue), step: max(1, step), onChange: onChange}
}

// Logical sizes: the − / + buttons (the track is as tall), the knob (a thin
// vertical line over the track's height) and the label line above the track.
// The fill's brightness depends on the distance from the knob: a glow that
// fades over about sliderFadeL (3× that, it is gone) on top of a tint of
// the accent that darkens from sliderTintNear next to the knob to
// sliderTintFar a whole track width away.
const (
	sliderBtnL     = 32
	sliderKnobWL   = 3
	sliderCaptionL = 24
	sliderFadeL    = 16
	sliderTintNear = 0.22
	sliderTintFar  = 0.08
)

func (sliderRow) height() int { return 64 }

// geometry returns the − button, the track's hit area, the + button and the
// track's value range (x of 0 and of max).
func (s sliderRow) geometry(c *canvas, r rect) (minus, track, plus rect, x0, x1 int32) {
	in := inset(c, r)
	size := c.px(sliderBtnL)
	top := r.Top + c.px(sliderCaptionL+2)
	minus = rect{in.Left, top, in.Left + size, top + size}
	plus = rect{in.Right - size, top, in.Right, top + size}
	track = rect{minus.Right + c.px(6), top, plus.Left - c.px(6), top + size}
	half := c.px(sliderKnobWL) / 2 // the knob reaches the track's ends
	return minus, track, plus, track.Left + half, track.Right - half
}

func (s sliderRow) segments(c *canvas, r rect) []rect {
	minus, track, plus, _, _ := s.geometry(c, r)
	return []rect{minus, track, plus}
}

func (s sliderRow) click(seg int) {
	if s.onChange == nil {
		return
	}
	switch seg {
	case 0:
		s.onChange(max(0, (s.pos-1)/s.step*s.step), true)
	case 2:
		s.onChange(min(s.max, (s.pos/s.step+1)*s.step), true)
	}
}
func (sliderRow) keepsOpen() bool { return true }

func (s sliderRow) dragIDAt(seg int) string {
	if seg != 1 {
		return ""
	}
	return "slider:" + s.id
}
func (s sliderRow) dragTo(c *canvas, r rect, _ string, x int32, done bool) {
	_, _, _, x0, x1 := s.geometry(c, r)
	v := s.pos
	if x1 > x0 {
		v = int((int64(x-x0)*int64(s.max) + int64(x1-x0)/2) / int64(x1-x0))
		v = max(0, min(v, s.max))
	}
	if s.onChange != nil && (v != s.pos || done) {
		s.onChange(v, done)
	}
}

func (s sliderRow) paint(c *canvas, r rect, st rowState) {
	in := inset(c, r)
	caption := rect{in.Left, r.Top + c.px(2), in.Right, r.Top + c.px(sliderCaptionL)}
	c.text(s.label, rect{caption.Left, caption.Top, caption.Right - c.px(60), caption.Bottom}, fontBody, colText, dtLeft)
	c.text(s.value, rect{caption.Right - c.px(60), caption.Top, caption.Right, caption.Bottom}, fontBold, colText, dtRight)

	minus, track, plus, x0, x1 := s.geometry(c, r)
	for _, b := range []struct {
		box   rect
		glyph string
		seg   int
	}{{minus, "−", 0}, {plus, "+", 2}} {
		bg := colControl
		if st.press == b.seg {
			bg = shade(bg, -18)
		} else if st.hover == b.seg {
			bg = shade(bg, 14)
		}
		c.roundRect(b.box, c.px(8), bg)
		c.text(b.glyph, b.box, fontBold, colText, dtCenter)
	}

	paintTrack(c, track, c.px(8), x0, x1, s.pos, s.max, st.hover == 1, st.press == 1)
}

// paintTrack draws a slider track with its fill and knob at pos of max;
// x0 and x1 are where the knob sits at 0 and at max. Shared by Slider and
// the audio flow's compact volume tracks.
func paintTrack(c *canvas, track rect, radius, x0, x1 int32, pos, maxValue int, hover, press bool) {
	// The track is filled from its left end to the knob, full accent at the
	// knob and darker with every pixel to the left. No part of the fill is
	// flat: where a flat colour meets a ramp, the eye sees a dark seam (Mach
	// banding). Measured from the knob, the whole gradient moves with it.
	c.roundRect(track, radius, colControl)
	kx := x0 + int32(int64(x1-x0)*int64(pos)/int64(max(1, maxValue)))
	if fill := (rect{track.Left, track.Top, kx, track.Bottom}); pos > 0 && fill.w() > 0 {
		n := max(8, int(fill.w()/max(1, c.px(3)))) // pieces of about 3 px
		stops := make([]uint32, n+1)
		fade, span := float64(c.px(sliderFadeL)), float64(track.w())
		for i := range stops {
			d := float64(fill.w()) * float64(n-i) / float64(n) // distance from the knob
			tint := sliderTintNear - (sliderTintNear-sliderTintFar)*min(1, d/span)
			stops[i] = mix(colControl, colAccent, tint+(1-tint)*math.Exp(-d/fade))
		}
		c.hGradientStops(fill, stops, track, radius)
	}
	// The knob: a thin line over the whole height (clipped to the rounded
	// ends), accent while dragged.
	kw, knobCol := c.px(sliderKnobWL), colText
	if press {
		knobCol = colAccent
	} else if hover {
		kw = c.px(sliderKnobWL + 1)
	}
	c.hGradient(rect{kx - kw/2, track.Top, kx - kw/2 + kw, track.Bottom}, knobCol, knobCol, track, radius)
}

// A volume track carries a small speaker icon at its right end, so it reads
// as a volume. Above trackIconFlip percent the knob would reach it there, so
// the icon moves to the left end (into the fill).
const (
	trackIconL    = 10 // logical size
	trackIconPadL = 3  // from the track's rounded end
	trackIconFlip = 90 // percent of the range
)

// trackIconBox is where paintTrackIcon draws the icon in track for pos of max.
func trackIconBox(c *canvas, track rect, pos, maxValue int) rect {
	size, pad := c.px(trackIconL), c.px(trackIconPadL)
	top := track.Top + (track.h()-size)/2
	if pos*100 > trackIconFlip*max(1, maxValue) {
		return rect{track.Left + pad, top, track.Left + pad + size, top + size}
	}
	return rect{track.Right - pad - size, top, track.Right - pad, top + size}
}

// paintTrackIcon draws the speaker icon of a volume track painted by
// paintTrack; hover brightens it with the knob.
func paintTrackIcon(c *canvas, track rect, pos, maxValue int, hover bool) {
	col := colMuted
	if hover {
		col = colText
	}
	c.icon(IconVolume, trackIconBox(c, track, pos, maxValue), trackIconL, col)
}

// mix blends two COLORREFs: t = 0 gives a, 1 gives b.
func mix(a, b uint32, t float64) uint32 {
	ch := func(shift uint) uint32 {
		x, y := float64(a>>shift&0xFF), float64(b>>shift&0xFF)
		return uint32(x+(y-x)*t+0.5) << shift
	}
	return ch(0) | ch(8) | ch(16)
}

// --- Warning -----------------------------------------------------------------

type warningRow struct {
	text, tipText string
	onClick       func()
}

// Warning is a short, clickable warning: a yellow outline around a
// warning icon and one line of text, with a chevron saying it leads on
// (e.g. to a help page). tip, shown on hover, says more. Clicking closes
// the popup, as it usually opens the browser.
func Warning(text, tip string, onClick func()) Row { return warningRow{text, tip, onClick} }

func (w warningRow) tip(*canvas, rect, int) string { return w.tipText }

func (warningRow) height() int { return 38 }
func (w warningRow) box(c *canvas, r rect) rect {
	in := inset(c, r)
	return rect{in.Left, r.Top + c.px(3), in.Right, r.Bottom - c.px(3)}
}
func (w warningRow) segments(c *canvas, r rect) []rect {
	if w.onClick == nil {
		return nil
	}
	return []rect{w.box(c, r)}
}
func (w warningRow) click(int) {
	if w.onClick != nil {
		w.onClick()
	}
}
func (warningRow) keepsOpen() bool { return false }
func (w warningRow) paint(c *canvas, r rect, st rowState) {
	box := w.box(c, r)
	tint := 0.07
	switch {
	case st.press == 0:
		tint = 0.17
	case st.hover == 0:
		tint = 0.12
	}
	c.frame(box, c.px(10), mix(colBackground, colWarning, tint), colWarning)
	iw, pad := c.px(buttonIconL), c.px(10)
	c.icon(IconAlert, rect{box.Left + pad, box.Top, box.Left + pad + iw, box.Bottom}, buttonIconL, colWarning)
	if w.onClick != nil {
		c.icon(IconChevronRight, rect{box.Right - pad - iw, box.Top, box.Right - pad, box.Bottom}, buttonIconL, colWarning)
	}
	c.text(w.text, w.textBox(c, box), fontBody, colText, dtLeft)
}

// textBox is where the text goes: between the icon and the chevron.
func (w warningRow) textBox(c *canvas, box rect) rect {
	iw, pad := c.px(buttonIconL), c.px(10)
	right := box.Right - pad
	if w.onClick != nil {
		right -= iw + c.px(5)
	}
	return rect{box.Left + pad + iw + c.px(8), box.Top, right, box.Bottom}
}

// --- Buttons -----------------------------------------------------------------

// Action is one button of a Buttons row.
type Action struct {
	Label    string
	Icon     Icon // optional, drawn before the label
	Style    ButtonStyle
	Disabled bool
	// Closes closes the panel before OnClick runs, e.g. for actions that
	// open a dialog or another window.
	Closes  bool
	OnClick func()
}

type buttonsRow struct{ actions []Action }

// Buttons puts several buttons side by side in one row, equally wide.
func Buttons(actions ...Action) Row { return buttonsRow{actions} }

func (buttonsRow) height() int { return 38 }

func (b buttonsRow) boxes(c *canvas, r rect) []rect {
	in := inset(c, r)
	n := int32(max(1, len(b.actions)))
	gap := c.px(8)
	w := (in.w() - gap*(n-1)) / n
	out := make([]rect, len(b.actions))
	for i := range b.actions {
		left := in.Left + int32(i)*(w+gap)
		out[i] = rect{left, r.Top + c.px(3), left + w, r.Bottom - c.px(3)}
	}
	if len(out) > 0 {
		out[len(out)-1].Right = in.Right // absorb rounding
	}
	return out
}

// segments keeps one entry per action so indexes match; a disabled
// button's empty rectangle can never be hit.
func (b buttonsRow) segments(c *canvas, r rect) []rect {
	boxes := b.boxes(c, r)
	for i, a := range b.actions {
		if a.Disabled {
			boxes[i] = rect{}
		}
	}
	return boxes
}

func (b buttonsRow) click(seg int) {
	if seg >= 0 && seg < len(b.actions) && b.actions[seg].OnClick != nil {
		b.actions[seg].OnClick()
	}
}
func (buttonsRow) keepsOpen() bool { return true }
func (b buttonsRow) keepsOpenAt(seg int) bool {
	return seg < 0 || seg >= len(b.actions) || !b.actions[seg].Closes
}

func (b buttonsRow) paint(c *canvas, r rect, st rowState) {
	for i, box := range b.boxes(c, r) {
		a := b.actions[i]
		paintButton(c, box, a.Label, a.Icon, a.Style, a.Disabled, st.hover == i, st.press == i)
	}
}

// --- Segmented ---------------------------------------------------------------

type segmentedRow struct {
	options  []string
	selected int
	disabled bool
	onSelect func(int)
}

// Segmented is a choice of options in one outlined group, equally wide,
// with the selected one highlighted. Disabled greys it out (the setting
// doesn't apply right now) and ignores clicks.
func Segmented(options []string, selected int, disabled bool, onSelect func(int)) Row {
	return segmentedRow{options, selected, disabled, onSelect}
}

func (segmentedRow) height() int     { return 40 }
func (segmentedRow) keepsOpen() bool { return true }

// frame is the group's outline; boxes are the options inside it.
func (s segmentedRow) frame(c *canvas, r rect) rect {
	in := inset(c, r)
	return rect{in.Left, r.Top + c.px(3), in.Right, r.Bottom - c.px(3)}
}

func (s segmentedRow) boxes(c *canvas, r rect) []rect {
	f := s.frame(c, r)
	pad := c.px(3)
	n := int32(max(1, len(s.options)))
	w := (f.w() - 2*pad) / n
	out := make([]rect, len(s.options))
	for i := range out {
		left := f.Left + pad + int32(i)*w
		out[i] = rect{left, f.Top + pad, left + w, f.Bottom - pad}
	}
	if len(out) > 0 {
		out[len(out)-1].Right = f.Right - pad
	}
	return out
}

func (s segmentedRow) segments(c *canvas, r rect) []rect {
	if s.disabled {
		return nil
	}
	return s.boxes(c, r)
}

func (s segmentedRow) click(seg int) {
	if seg >= 0 && seg < len(s.options) && seg != s.selected && s.onSelect != nil {
		s.onSelect(seg)
	}
}

func (s segmentedRow) paint(c *canvas, r rect, st rowState) {
	border, selFill, selText, text := colControl, mix(colBackground, colAccent, 0.2), colAccent, colMuted
	if s.disabled {
		border, selFill, selText, text = colDivider, colRowHover, colCaption, colCaption
	}
	c.frame(s.frame(c, r), c.px(10), colBackground, border)
	for i, box := range s.boxes(c, r) {
		fg := text
		switch {
		case i == s.selected:
			c.roundRect(box, c.px(8), selFill)
			fg = selText
		case !s.disabled && st.press == i:
			c.roundRect(box, c.px(8), colRowPress)
			fg = colText
		case !s.disabled && st.hover == i:
			c.roundRect(box, c.px(8), colRowHover)
			fg = colText
		}
		c.text(s.options[i], box, fontBold, fg, dtCenter)
	}
}

// --- Paragraph ---------------------------------------------------------------

type paragraphRow struct {
	text string
	tone Tone
}

// Paragraph is word-wrapped text of any length; the row grows to fit it.
func Paragraph(text string, tone Tone) Row { return paragraphRow{text, tone} }

func (paragraphRow) height() int                   { return 22 }
func (paragraphRow) segments(*canvas, rect) []rect { return nil }
func (paragraphRow) click(int)                     {}
func (paragraphRow) keepsOpen() bool               { return true }
func (p paragraphRow) measure(c *canvas, width int32) int32 {
	h := c.wrapHeight(p.text, width-2*c.px(padX), fontBody)
	if h == 0 {
		return c.px(p.height())
	}
	return h + c.px(8)
}
func (p paragraphRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	c.textWrapped(p.text, rect{in.Left, r.Top + c.px(3), in.Right, r.Bottom}, fontBody, p.tone.color())
}

// --- Guided-setup header -----------------------------------------------------

type wizardHeaderRow struct {
	title       string
	step, total int
	onBack      func()
}

// WizardHeader is the top row of a guided-setup page: a back button (when
// onBack is set), the title, and progress dots for step of total (1-based).
func WizardHeader(title string, step, total int, onBack func()) Row {
	return wizardHeaderRow{title, step, total, onBack}
}

func (wizardHeaderRow) height() int { return 46 }
func (h wizardHeaderRow) segments(c *canvas, r rect) []rect {
	if h.onBack == nil {
		return nil
	}
	return []rect{backButton(c, r)}
}
func (h wizardHeaderRow) click(int) {
	if h.onBack != nil {
		h.onBack()
	}
}
func (wizardHeaderRow) keepsOpen() bool { return true }
func (h wizardHeaderRow) back() bool    { return runBack(h.onBack) }
func (h wizardHeaderRow) paint(c *canvas, r rect, st rowState) {
	in := inset(c, r)
	left := in.Left
	if h.onBack != nil {
		back := backButton(c, r)
		paintBack(c, back, st)
		left = back.Right + c.px(8)
	}
	// Dots: done and current in the accent colour, the current one wider.
	dot, gap, wide := c.px(8), c.px(5), c.px(18)
	x := in.Right
	for i := h.total; i >= 1; i-- {
		w, col := dot, colControl
		if i <= h.step {
			col = colAccent
		}
		if i == h.step {
			w = wide
		}
		cy := r.Top + r.h()/2
		if w == dot {
			c.circle(x-dot/2, cy, dot/2, col) // RoundRect turns tiny dots into diamonds
		} else {
			c.roundRect(rect{x - w, cy - dot/2, x, cy - dot/2 + dot}, dot, col)
		}
		x -= w + gap
	}
	c.text(h.title, headerTitle(c, r, left, x-c.px(8)), fontTitle, colText, dtLeft)
}

// --- Nav with a tool button ----------------------------------------------------

type navToolRow struct {
	nav     navRow
	icon    Icon
	toolTip string
	onTool  func()
}

// NavTool is a Nav with a square icon button on its left, such as a
// refresh button beside the library. tip is the button's tooltip; a nil
// onTool shows the button disabled.
func NavTool(icon Icon, tip string, onTool func(), label, detail string, tone Tone, onClick func()) Row {
	return navToolRow{navRow{label, detail, tone, onClick}, icon, tip, onTool}
}

func (navToolRow) height() int { return 38 }

func (n navToolRow) geometry(c *canvas, r rect) (box, tool rect) {
	full := navBox(c, r)
	tool = rect{full.Left, full.Top, full.Left + full.h(), full.Bottom}
	return rect{tool.Right + c.px(8), full.Top, full.Right, full.Bottom}, tool
}

func (n navToolRow) segments(c *canvas, r rect) []rect {
	box, tool := n.geometry(c, r)
	if n.onTool == nil {
		tool = rect{}
	}
	return []rect{box, tool}
}

func (n navToolRow) click(seg int) {
	switch {
	case seg == 0 && n.nav.onClick != nil:
		n.nav.onClick()
	case seg == 1 && n.onTool != nil:
		n.onTool()
	}
}
func (navToolRow) keepsOpen() bool { return true }

func (n navToolRow) tip(_ *canvas, _ rect, seg int) string {
	if seg == 1 {
		return n.toolTip
	}
	return ""
}

func (n navToolRow) paint(c *canvas, r rect, st rowState) {
	box, tool := n.geometry(c, r)
	n.nav.paintIn(c, box, st.hover == 0, st.press == 0)
	paintButton(c, tool, "", n.icon, ButtonNormal, n.onTool == nil, st.hover == 1, st.press == 1)
}

// --- Brand header ----------------------------------------------------------------

type brandRow struct {
	logo                     *Image
	title, version, subtitle string
	progress                 func() float64
	update                   *BrandUpdate
}

// BrandUpdate is the badge in the Brand header's top right corner: an
// accent dot with a short label, e.g. when an update is available.
type BrandUpdate struct {
	Label   string // e.g. "Update"
	Tip     string // tooltip
	OnClick func()
}

// Brand is the home page header: the app logo, a letter-spaced title with
// the version after it (may be empty) and a status line. logo and update
// may be nil. progress, if set, is read on every paint and drawn as a thin,
// short bar between the title and the status line (0..1; negative = none),
// so Popup.Repaint animates it without rebuilding the rows. The status
// stays put whether the bar shows or not.
func Brand(logo *Image, title, version, subtitle string, progress func() float64, update *BrandUpdate) Row {
	return brandRow{logo, title, version, subtitle, progress, update}
}

const (
	brandLogoL  = 36
	brandDotL   = 4 // radius of the update dot
	brandBadgeH = 24
	brandBarL   = 2 // the progress bar's top, below the middle of the row
)

func (brandRow) height() int { return 59 }

// titleBand is the upper half of the text area, where the title sits.
func (b brandRow) titleBand(c *canvas, r rect) rect {
	in := inset(c, r)
	left := in.Left
	if b.logo != nil {
		left += c.px(brandLogoL) + c.px(11)
	}
	return rect{left, r.Top + c.px(6), in.Right, r.Top + r.h()/2 + c.px(3)}
}

// badge is the update badge's box at the right end of the title band.
func (b brandRow) badge(c *canvas, r rect) rect {
	band := b.titleBand(c, r)
	w := c.px(8) + 2*c.px(brandDotL) + c.px(6) + c.textWidth(b.update.Label, fontCaption) + c.px(10)
	cy := band.Top + band.h()/2
	h := c.px(brandBadgeH)
	return rect{band.Right - w, cy - h/2, band.Right, cy - h/2 + h}
}

func (b brandRow) segments(c *canvas, r rect) []rect {
	if b.update == nil {
		return nil
	}
	return []rect{b.badge(c, r)}
}

func (b brandRow) click(int) {
	if b.update != nil && b.update.OnClick != nil {
		b.update.OnClick()
	}
}
func (brandRow) keepsOpen() bool { return true }

func (b brandRow) tip(_ *canvas, _ rect, seg int) string {
	if seg == 0 && b.update != nil {
		return b.update.Tip
	}
	return ""
}

func (b brandRow) paint(c *canvas, r rect, st rowState) {
	in := inset(c, r)
	if b.logo != nil {
		size := c.px(brandLogoL)
		c.image(b.logo, in.Left, r.Top+(r.h()-size)/2, brandLogoL)
	}
	band := b.titleBand(c, r)
	left := band.Left
	mid := r.Top + r.h()/2
	if b.update != nil {
		badge := b.badge(c, r)
		band.Right = badge.Left - c.px(6)
		switch {
		case st.press == 0:
			c.roundRect(badge, badge.h(), colRowPress)
		case st.hover == 0:
			c.roundRect(badge, badge.h(), colRowHover)
		}
		dot := c.px(brandDotL)
		cx, cy := badge.Left+c.px(8)+dot, badge.Top+badge.h()/2
		c.circle(cx, cy, dot, colAccent)
		c.text(b.update.Label, rect{cx + dot + c.px(6), badge.Top, badge.Right, badge.Bottom}, fontCaption, colAccent, dtLeft)
	}
	procSetTextCharExtra.Call(c.dc, uintptr(c.px(2))) // tracking for the capitals
	titleW := c.textWidth(b.title, fontTitle)         // includes the tracking
	c.text(b.title, band, fontTitle, colText, dtLeft)
	procSetTextCharExtra.Call(c.dc, 0)
	if b.version != "" {
		vx := band.Left + titleW + c.px(6)
		c.text(b.version, rect{vx, band.Top + c.px(3), band.Right, band.Bottom}, fontCaption, colCaption, dtLeft)
	}
	c.text(b.subtitle, rect{left, mid + c.px(4), in.Right, r.Bottom - c.px(6)}, fontCaption, colCaption, dtLeft)
	if b.progress == nil {
		return
	}
	if p := b.progress(); p >= 0 {
		h := max(2, c.px(3))
		top := mid + c.px(brandBarL)
		bar := rect{left, top, left + (in.Right-left)/2, top + h}
		c.roundRect(bar, h, colControl)
		if fill := int32(float64(bar.w()) * min(p, 1)); fill > 0 {
			c.roundRect(rect{bar.Left, bar.Top, bar.Left + max(fill, h), bar.Bottom}, h, colAccent)
		}
	}
}

// --- Drop zone -------------------------------------------------------------------

type dropZoneRow struct {
	title, hint string
	onClick     func()
}

// DropZone is a dashed field that invites dragging files or folders onto
// the panel (see Popup.SetAcceptDrops); clicking it runs onClick, e.g. a picker.
func DropZone(title, hint string, onClick func()) Row { return dropZoneRow{title, hint, onClick} }

func (dropZoneRow) height() int { return 100 }
func (d dropZoneRow) box(c *canvas, r rect) rect {
	in := inset(c, r)
	return rect{in.Left, r.Top + c.px(4), in.Right, r.Bottom - c.px(4)}
}
func (d dropZoneRow) segments(c *canvas, r rect) []rect { return []rect{d.box(c, r)} }
func (d dropZoneRow) click(int) {
	if d.onClick != nil {
		d.onClick()
	}
}
func (dropZoneRow) keepsOpen() bool { return true }
func (d dropZoneRow) paint(c *canvas, r rect, st rowState) {
	box := d.box(c, r)
	border := colMuted
	switch {
	case st.press == 0:
		c.roundRect(box, c.px(12), colRowPress)
		border = colAccent
	case st.hover == 0:
		c.roundRect(box, c.px(12), colRowHover)
		border = colAccent
	}
	c.dashedFrame(box, c.px(12), border)
	iconBox := rect{box.Left, box.Top + c.px(12), box.Right, box.Top + c.px(40)}
	c.icon(IconFolderPlus, iconBox, 24, colAccent)
	c.text(d.title, rect{box.Left, iconBox.Bottom + c.px(2), box.Right, iconBox.Bottom + c.px(22)}, fontBold, colText, dtCenter)
	c.text(d.hint, rect{box.Left, iconBox.Bottom + c.px(22), box.Right, iconBox.Bottom + c.px(40)}, fontCaption, colMuted, dtCenter)
}
