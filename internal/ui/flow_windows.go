package ui

// The audio flow: a small diagram of where audio goes, built from four rows
// stacked like any others. At the top the microphone (full width) and the
// soundboard (right column). The soundboard plays two streams at once: one
// into VB-Cable, one to the user's headphones. VB-Cable is drawn as a cable
// with two 3.5 mm jacks at its upper end: one plugged into the microphone
// (Windows' "Listen to this device"), one into the soundboard. Its far end
// is "CABLE Output", the microphone others' apps listen to.
//
//	YOUR MICROPHONE
//	[ Headset mic                    v ]
//	[▮▮▮▮▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯]   level bars dock under their control
//	 ╽                   SOUNDBOARD
//	 │ (mic badge)       [▶ Test sound   ]
//	 ├──(cable badge)──╼ [▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯]
//	 │ (cable badge)       (you badge) │
//	 ╿                                 v
//	OTHERS HEAR   100%   YOU HEAR     80%
//	[🎤ⓘ CABLE Output ]  [Speakers      v]
//	[▮▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯]   [▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯]
//	[=======|        ]   [=====|         ]
//
// The rows share the column and lane geometry, so the pipes join across row
// boundaries. Pipes light up with the signal passing through them, read on
// every paint (Popup.Repaint animates them). A pipe that should carry audio
// but doesn't is drawn broken, with a badge that explains why (tooltip) and
// leads to the fix (click).

// PipeState is how a connection of the audio flow is doing.
type PipeState int

const (
	PipeOn     PipeState = iota // connected; lights up while audio passes
	PipeOff                     // not in use, e.g. no microphone: faint and dashed
	PipeHeld                    // interrupted on purpose for now, e.g. the mic muted while sounds play
	PipeBroken                  // should be connected but isn't: drawn in the badge's tone
)

// Pipe is one connection of the audio flow.
type Pipe struct {
	State PipeState
	// Level is the signal passing through (0..1), read on every paint;
	// nil for none.
	Level func() float64
	// Badge, if set, sits on the pipe: its icon on the line, its label
	// beside it.
	Badge *Badge
}

// Badge marks a pipe with an icon and a short label. Hovering it shows
// Tip; clicking it runs OnClick (e.g. opens the page that fixes it).
type Badge struct {
	Icon    Icon
	Label   string
	Tone    Tone
	Tip     string
	OnClick func()
}

// FlowDevice is a device choice in the flow: a caption, a dropdown and a
// live level bar, optionally with a volume track.
type FlowDevice struct {
	ID          string // dropdown id: unique on the page, stable across renders
	Caption     string // e.g. "Microphone"
	Value       string // right side of the caption, e.g. "80%"
	Options     []Option
	Selected    int // index into Options, -1 shows Placeholder
	Placeholder string
	OnSelect    func(int)
	// Level is the meter's fill (0..1), read on every paint; nil leaves
	// the meter empty.
	Level  func() float64
	Volume *FlowVolume // outputs only
}

// FlowVolume is a compact volume track under an output's meter.
type FlowVolume struct {
	Value    int // 0..100
	Tip      string
	OnChange func(value int, done bool)
}

// FlowSounds is the soundboard: Resound's own sounds entering the flow, with
// a button that plays the test tone, or stops what plays.
type FlowSounds struct {
	Caption, Status string
	Playing         bool // the button stops instead of testing
	OnTest, OnStop  func()
	Level           func() float64
}

// FlowCable is the cable's far end: a fixed card (not a choice) with the
// name others' apps know it by, e.g. "CABLE Output", an info icon, a level
// bar and the volume of the sounds sent into it.
type FlowCable struct {
	Caption, Value string
	Name           string // "" shows Placeholder, muted
	Placeholder    string
	Info           string // the info icon's tooltip, e.g. where to pick it
	Tip            string // the card's tooltip, when clicking it does something
	OnClick        func() // the card's click, e.g. install VB-Cable
	Level          func() float64
	Volume         *FlowVolume
}

// AudioFlow is the whole diagram. Rows returns its rows, to be placed on a
// page like any others.
type AudioFlow struct {
	Mic    FlowDevice
	Sounds FlowSounds
	Cable  FlowCable  // what others hear
	You    FlowDevice // the user's headphones or speakers

	MicPipe   Pipe // the cable's jack in the microphone ("Listen to this device")
	CablePipe Pipe // the cable's jack in the soundboard
	CableLine Pipe // the cable itself: mic and sounds together, to CABLE Output
	YouPipe   Pipe // soundboard → you hear
}

// Rows returns the flow's rows: the microphone, the soundboard with the
// cable's jacks, the cable, and the two outputs.
func (f AudioFlow) Rows() []Row {
	return []Row{
		flowMicRow{f.Mic},
		flowSoundsRow{f.Sounds, f.MicPipe, f.CablePipe, f.CableLine},
		flowCableRow{f.CableLine, f.YouPipe},
		flowOutputsRow{f.Cable, f.You},
	}
}

// --- geometry and painting shared by the rows ----------------------------------

// Logical sizes.
const (
	flowColGapL    = 12 // between the two columns
	flowLaneInL    = 18 // the cable's distance from the left edge
	flowFieldL     = 34 // dropdowns, the cable card and the test button
	flowMeterL     = 6  // level bar docked under a control
	flowRadiusL    = 10 // corner radius of a control with its level bar
	flowBadgeL     = 20 // badge height
	flowBadgeIconL = 14
)

// flowPipePx is the width of pipes and the cable in device pixels, the
// same at every scale, like the jacks drawn onto them.
const flowPipePx = 3

// flowCols returns the left and right column of a row.
func flowCols(c *canvas, r rect) (left, right rect) {
	in := inset(c, r)
	w := (in.w() - c.px(flowColGapL)) / 2
	return rect{in.Left, r.Top, in.Left + w, r.Bottom}, rect{in.Right - w, r.Top, in.Right, r.Bottom}
}

// flowLanes returns the x of the cable (left column) and of the
// soundboard's stream to the headphones (right column), each as far from
// its column's left edge.
func flowLanes(c *canvas, r rect) (cable, toYou int32) {
	lc, rc := flowCols(c, r)
	return lc.Left + c.px(flowLaneInL), rc.Left + c.px(flowLaneInL)
}

// pipeColor is a pipe's colour and whether it is dashed. A connected pipe
// brightens towards the accent with the signal passing through.
func pipeColor(p Pipe) (col uint32, dashed bool) {
	idle := rgb(0x4A, 0x4D, 0x57)
	switch p.State {
	case PipeOff:
		return colDivider, true
	case PipeHeld:
		return colMuted, true
	case PipeBroken:
		tone := colDanger
		if p.Badge != nil && p.Badge.Tone != ToneNormal {
			tone = p.Badge.Tone.color()
		}
		return mix(colBackground, tone, 0.6), false
	}
	if p.Level == nil {
		return idle, false
	}
	return mix(idle, colAccent, min(1, p.Level()*1.4)), false
}

// pipeV draws a vertical pipe from y0 to y1 at x.
func pipeV(c *canvas, x, y0, y1 int32, p Pipe) {
	col, dashed := pipeColor(p)
	w := int32(flowPipePx)
	if !dashed {
		c.fill(rect{x - w/2, y0, x - w/2 + w, y1}, col)
		return
	}
	dash, gap := c.px(4), c.px(3)
	for y := y0; y < y1; y += dash + gap {
		c.fill(rect{x - w/2, y, x - w/2 + w, min(y+dash, y1)}, col)
	}
}

// pipeH draws a horizontal pipe from x0 to x1 at y.
func pipeH(c *canvas, x0, x1, y int32, p Pipe) {
	col, dashed := pipeColor(p)
	w := int32(flowPipePx)
	if !dashed {
		c.fill(rect{x0, y - w/2, x1, y - w/2 + w}, col)
		return
	}
	dash, gap := c.px(4), c.px(3)
	for x := x0; x < x1; x += dash + gap {
		c.fill(rect{x, y - w/2, min(x+dash, x1), y - w/2 + w}, col)
	}
}

// vGapped draws a vertical pipe that leaves a gap where its badge sits.
func vGapped(c *canvas, x, y0, y1 int32, badge rect, p Pipe) {
	if badge.h() == 0 {
		pipeV(c, x, y0, y1, p)
		return
	}
	pipeV(c, x, y0, badge.Top, p)
	pipeV(c, x, badge.Bottom, y1, p)
}

// hGapped draws a horizontal pipe that leaves a gap where its badge sits.
func hGapped(c *canvas, x0, x1, y int32, badge rect, p Pipe) {
	if badge.w() == 0 {
		pipeH(c, x0, x1, y, p)
		return
	}
	pipeH(c, x0, badge.Left, y, p)
	pipeH(c, badge.Right, x1, y, p)
}

// badgeWidth is a badge's natural width.
func badgeWidth(c *canvas, b *Badge) int32 {
	w := c.px(6) + c.px(flowBadgeIconL) + c.px(6)
	if b.Label != "" {
		w += c.px(5) + c.textWidth(b.Label, fontCaption) + c.px(2)
	}
	return w
}

// badgeOnLane is the box of a badge whose icon sits on the lane at x,
// centred on cy, its label to the right up to maxRight.
func badgeOnLane(c *canvas, b *Badge, x, cy, maxRight int32) rect {
	h := c.px(flowBadgeL)
	left := x - c.px(flowBadgeIconL)/2 - c.px(6)
	return rect{left, cy - h/2, min(left+badgeWidth(c, b), maxRight), cy - h/2 + h}
}

// badgeOnLaneLeft is badgeOnLane with the label to the left, down to minLeft
// (for the headphones' lane). paintBadge puts the icon on the right then.
func badgeOnLaneLeft(c *canvas, b *Badge, x, cy, minLeft int32) rect {
	h := c.px(flowBadgeL)
	right := x + c.px(flowBadgeIconL)/2 + c.px(6)
	return rect{max(right-badgeWidth(c, b), minLeft), cy - h/2, right, cy - h/2 + h}
}

// badgeCentred is the box of a badge centred on (cx, cy), no wider than maxW.
func badgeCentred(c *canvas, b *Badge, cx, cy, maxW int32) rect {
	w, h := min(badgeWidth(c, b), maxW), c.px(flowBadgeL)
	return rect{cx - w/2, cy - h/2, cx - w/2 + w, cy - h/2 + h}
}

// paintBadge draws a badge: a pill in the panel's background colour (so it
// covers the pipe), the icon and the label in the badge's tone. iconRight
// puts the icon at the right end (badgeOnLaneLeft).
func paintBadge(c *canvas, box rect, b *Badge, hover, press, iconRight bool) {
	bg := colBackground
	if b.OnClick != nil || b.Tip != "" {
		switch {
		case press:
			bg = colRowPress
		case hover:
			bg = colRowHover
		}
	}
	c.roundRect(box, box.h(), bg)
	col := b.Tone.color()
	if b.Tone == ToneNormal {
		col = colMuted
	}
	iw, pad := c.px(flowBadgeIconL), c.px(6)
	ic := rect{box.Left + pad, box.Top, box.Left + pad + iw, box.Bottom}
	label := rect{ic.Right + c.px(5), box.Top, box.Right - pad, box.Bottom}
	align := uint32(dtLeft)
	if iconRight {
		ic = rect{box.Right - pad - iw, box.Top, box.Right - pad, box.Bottom}
		label = rect{box.Left + pad, box.Top, ic.Left - c.px(5), box.Bottom}
		align = dtRight
	}
	c.icon(b.Icon, ic, flowBadgeIconL, col)
	if b.Label != "" {
		c.text(b.Label, label, fontCaption, col, align)
	}
}

// badgeTip is a badge's tooltip, "" without one.
func badgeTip(b *Badge) string {
	if b == nil {
		return ""
	}
	return b.Tip
}

func clickBadge(b *Badge) {
	if b != nil && b.OnClick != nil {
		b.OnClick()
	}
}

// jackDir is where a jack's tip points.
type jackDir int

const (
	jackUp jackDir = iota
	jackRight
	jackDown
)

// jackRows is the 3.5 mm jack as pixel art, one entry per device pixel
// from the tip: the width of that row, centred on the cable (0 = a gap; a
// negative width is a bevel: its middle solid, its outer pixels half
// blended into the background, a diagonal). A 1-pixel tip and the contact
// part (6 pixels with the tip) with a divider at its fourth, then a bevel
// out to a 5×5 body and one back into the 3-pixel cable. It is drawn the
// same at every scale, like the cable.
var jackRows = []int32{1, 3, 3, 0, 3, 3, -5, 5, 5, 5, 5, 5, -5}

// jackLen is a jack's length in device pixels, tip to the end of its body.
var jackLen = int32(len(jackRows))

// paintJack draws a jack whose tip is at (x, y), pointing dir.
func paintJack(c *canvas, x, y int32, dir jackDir, col uint32) {
	// span fills row a (0 = tip) from u0 to u1 across the jack, measured
	// from the cable's centre line.
	span := func(a, u0, u1 int32, col uint32) {
		switch dir {
		case jackRight:
			c.fill(rect{x - a - 1, y + u0, x - a, y + u1}, col)
		case jackDown:
			c.fill(rect{x + u0, y - a - 1, x + u1, y - a}, col)
		default:
			c.fill(rect{x + u0, y + a, x + u1, y + a + 1}, col)
		}
	}
	soft := mix(colBackground, col, 0.5)
	for i, w := range jackRows {
		a := int32(i)
		switch {
		case w > 0:
			span(a, -w/2, -w/2+w, col)
		case w < 0: // bevel: solid middle, half-blended ends
			w = -w
			span(a, -w/2+1, w/2, col)
			span(a, -w/2, -w/2+1, soft)
			span(a, w/2, w/2+1, soft)
		}
	}
}

// dockedUnit is a control with its level bar docked under it: one rounded
// shape, the bar along its bottom.
func dockedUnit(c *canvas, control rect) (unit, bar rect) {
	bar = rect{control.Left, control.Bottom, control.Right, control.Bottom + c.px(flowMeterL)}
	return rect{control.Left, control.Top, control.Right, bar.Bottom}, bar
}

// paintDocked draws the background of a control with its docked level bar:
// the control's area in bg, the bar below it, clipped to their rounded
// shape. The control's face is drawn on top afterwards.
func paintDocked(c *canvas, control rect, bg uint32, level float64) {
	unit, bar := dockedUnit(c, control)
	restore := c.clipRound(unit, c.px(flowRadiusL))
	c.fill(control, bg)
	paintMeterBar(c, bar, level)
	restore()
}

// paintMeterBar fills a level bar: small blocks like Windows' own meters,
// green, yellow over the last stretch, red at the top.
func paintMeterBar(c *canvas, bar rect, level float64) {
	c.fill(bar, rgb(0x2B, 0x2D, 0x34))
	if level <= 0 {
		return
	}
	bw, gap := max(2, c.px(3)), max(1, c.px(1))
	end := bar.Left + int32(float64(bar.w())*min(1, level))
	for x := bar.Left; x < end; x += bw + gap {
		col := colAccent
		switch f := float64(x-bar.Left) / float64(bar.w()); {
		case f >= 0.98:
			col = colDanger
		case f >= 0.89:
			col = colWarning
		}
		c.fill(rect{x, bar.Top, min(x+bw, end), bar.Bottom}, col)
	}
}

// paintDockedField draws a dropdown field with its level bar docked under it.
func paintDockedField(c *canvas, field rect, f dropdownField, level float64, open, hover, press bool) {
	paintDocked(c, field, fieldBg(open, hover, press), level)
	if open {
		unit, _ := dockedUnit(c, field)
		c.outline(unit, c.px(flowRadiusL), colAccent)
	}
	paintFieldContent(c, field, f, open)
}

func readLevel(f func() float64) float64 {
	if f == nil {
		return 0
	}
	return f()
}

// paintCaption draws a column caption with an optional value on the right.
func paintCaption(c *canvas, box rect, caption, value string) {
	right := box.Right
	if value != "" {
		w := c.textWidth(value, fontCaption) + c.px(2)
		c.text(value, rect{box.Right - w, box.Top, box.Right, box.Bottom}, fontCaption, colMuted, dtRight)
		right -= w + c.px(6)
	}
	c.text(upper(caption), rect{box.Left, box.Top, right, box.Bottom}, fontCaption, colCaption, dtLeft)
}

func (d FlowDevice) field() dropdownField {
	ph := d.Placeholder
	if ph == "" {
		ph = "Choose…"
	}
	return dropdownField{id: d.ID, options: d.Options, selected: d.Selected, placeholder: ph, onSelect: d.OnSelect}
}

// --- microphone ----------------------------------------------------------------

type flowMicRow struct{ dev FlowDevice }

// The row ends with the level bar: the cable's jack starts right below.
func (flowMicRow) height() int { return 22 + flowFieldL + flowMeterL }

func (m flowMicRow) geometry(c *canvas, r rect) (caption, field rect) {
	in := inset(c, r)
	caption = rect{in.Left, r.Top + c.px(2), in.Right, r.Top + c.px(20)}
	field = rect{in.Left, r.Top + c.px(22), in.Right, r.Top + c.px(22+flowFieldL)}
	return
}

func (m flowMicRow) segments(c *canvas, r rect) []rect {
	_, field := m.geometry(c, r)
	return []rect{field}
}
func (flowMicRow) click(int)       {}
func (flowMicRow) keepsOpen() bool { return true }

func (m flowMicRow) field(seg int) (dropdownField, bool) { return m.dev.field(), seg == 0 }

func (m flowMicRow) tip(c *canvas, r rect, _ int) string {
	_, field := m.geometry(c, r)
	return fieldTip(c, field, m.dev.field())
}

func (m flowMicRow) paint(c *canvas, r rect, st rowState) {
	caption, field := m.geometry(c, r)
	paintCaption(c, caption, m.dev.Caption, m.dev.Value)
	paintDockedField(c, field, m.dev.field(), readLevel(m.dev.Level), st.open == 0, st.hover == 0, st.press == 0)
}

// --- soundboard and the cable's two jacks -----------------------------------------

type flowSoundsRow struct {
	s                FlowSounds
	mic, cable, line Pipe
}

// The row ends with the soundboard's level bar: the headphones' jack
// starts right below, like the cable's below the microphone.
func (flowSoundsRow) height() int { return 26 + flowFieldL + flowMeterL }

// geometry: the soundboard's caption and button (its level bar docks
// below), the y where the cable's two ends join, and the mic and cable
// badges (empty without one).
func (s flowSoundsRow) geometry(c *canvas, r rect) (caption, button rect, joinY int32, micBadge, cableBadge rect) {
	_, rc := flowCols(c, r)
	caption = rect{rc.Left, r.Top + c.px(8), rc.Right, r.Top + c.px(24)}
	button = rect{rc.Left, r.Top + c.px(26), rc.Right, r.Top + c.px(26+flowFieldL)}
	joinY = button.Top + button.h()/2
	lane, _ := flowLanes(c, r)
	if b := s.mic.Badge; b != nil {
		micBadge = badgeOnLane(c, b, lane, (r.Top+jackLen+joinY)/2, rc.Left-c.px(10))
	}
	if b := s.cable.Badge; b != nil {
		x0, x1 := lane+c.px(4), rc.Left-jackLen
		cableBadge = badgeCentred(c, b, (x0+x1)/2, joinY, x1-x0-c.px(8))
	}
	return
}

// segments: the button, the mic badge, the cable badge.
func (s flowSoundsRow) segments(c *canvas, r rect) []rect {
	_, button, _, mic, cable := s.geometry(c, r)
	return []rect{button, mic, cable}
}

func (s flowSoundsRow) click(seg int) {
	switch {
	case seg == 0 && s.s.Playing && s.s.OnStop != nil:
		s.s.OnStop()
	case seg == 0 && !s.s.Playing && s.s.OnTest != nil:
		s.s.OnTest()
	case seg == 1:
		clickBadge(s.mic.Badge)
	case seg == 2:
		clickBadge(s.cable.Badge)
	}
}
func (flowSoundsRow) keepsOpen() bool { return true }

func (s flowSoundsRow) tip(_ *canvas, _ rect, seg int) string {
	switch seg {
	case 1:
		return badgeTip(s.mic.Badge)
	case 2:
		return badgeTip(s.cable.Badge)
	}
	return ""
}

func (s flowSoundsRow) paint(c *canvas, r rect, st rowState) {
	caption, button, joinY, micBadge, cableBadge := s.geometry(c, r)
	paintCaption(c, caption, s.s.Caption, s.s.Status)
	label, icon := "Test sound", IconPlay
	if s.s.Playing {
		label, icon = "Stop", IconStop
	}
	bg, fg := buttonColors(ButtonNormal, false, st.hover == 0, st.press == 0)
	paintDocked(c, button, bg, readLevel(s.s.Level))
	paintButtonFace(c, button, label, icon, fg)

	lane, _ := flowLanes(c, r)
	jack := jackLen
	// The cable's jack in the microphone, and its pipe down to the join.
	micCol, _ := pipeColor(s.mic)
	paintJack(c, lane, r.Top, jackUp, micCol)
	vGapped(c, lane, r.Top+jack, joinY, micBadge, s.mic)
	// Its jack in the soundboard, and its pipe across to the join.
	cableCol, _ := pipeColor(s.cable)
	paintJack(c, button.Left, joinY, jackRight, cableCol)
	hGapped(c, lane, button.Left-jack, joinY, cableBadge, s.cable)
	// From the join on, the cable carries both, down into the next row.
	pipeV(c, lane, joinY-flowPipePx/2, r.Bottom, s.line)
	if s.mic.Badge != nil {
		paintBadge(c, micBadge, s.mic.Badge, st.hover == 1, st.press == 1, false)
	}
	if s.cable.Badge != nil {
		paintBadge(c, cableBadge, s.cable.Badge, st.hover == 2, st.press == 2, false)
	}
}

// --- the cable and the headphones' stream down to the outputs -----------------------

// flowCableRow carries the cable down to its far end, and the soundboard's
// stream to the headphones from a jack in the soundboard's level bar to one
// at "You hear". Both lanes have their badges to the right.
type flowCableRow struct {
	cable, you Pipe
}

func (flowCableRow) height() int { return 52 }

// tipY is where the jacks at the outputs end, just above their captions.
func (flowCableRow) tipY(c *canvas, r rect) int32 { return r.Bottom - c.px(3) }

// badges returns the boxes of the cable and you badges (empty where a pipe
// has none), between the jacks.
func (j flowCableRow) badges(c *canvas, r rect) [2]rect {
	var out [2]rect
	lane, toYou := flowLanes(c, r)
	in := inset(c, r)
	cy := (r.Top + j.tipY(c, r)) / 2
	if b := j.cable.Badge; b != nil {
		out[0] = badgeOnLane(c, b, lane, cy, toYou-c.px(10))
	}
	if b := j.you.Badge; b != nil {
		out[1] = badgeOnLane(c, b, toYou, cy, in.Right)
	}
	return out
}

func (j flowCableRow) segments(c *canvas, r rect) []rect {
	b := j.badges(c, r)
	return b[:]
}

func (j flowCableRow) click(seg int) {
	switch seg {
	case 0:
		clickBadge(j.cable.Badge)
	case 1:
		clickBadge(j.you.Badge)
	}
}
func (flowCableRow) keepsOpen() bool { return true }

func (j flowCableRow) tip(_ *canvas, _ rect, seg int) string {
	switch seg {
	case 0:
		return badgeTip(j.cable.Badge)
	case 1:
		return badgeTip(j.you.Badge)
	}
	return ""
}

func (j flowCableRow) paint(c *canvas, r rect, st rowState) {
	lane, toYou := flowLanes(c, r)
	boxes := j.badges(c, r)
	tip := j.tipY(c, r)

	// The cable down to its jack at the far end, CABLE Output.
	vGapped(c, lane, r.Top, tip-jackLen, boxes[0], j.cable)
	col, _ := pipeColor(j.cable)
	paintJack(c, lane, tip, jackDown, col)
	// The headphones' stream: a jack in the soundboard's level bar, one at
	// "You hear".
	col, _ = pipeColor(j.you)
	paintJack(c, toYou, r.Top, jackUp, col)
	vGapped(c, toYou, r.Top+jackLen, tip-jackLen, boxes[1], j.you)
	paintJack(c, toYou, tip, jackDown, col)

	if j.cable.Badge != nil {
		paintBadge(c, boxes[0], j.cable.Badge, st.hover == 0, st.press == 0, false)
	}
	if j.you.Badge != nil {
		paintBadge(c, boxes[1], j.you.Badge, st.hover == 1, st.press == 1, false)
	}
}

// --- outputs ---------------------------------------------------------------------

type flowOutputsRow struct {
	cable FlowCable
	you   FlowDevice
}

func (flowOutputsRow) height() int { return 86 }

// flowColumn is one output's caption, card or field (its level bar docks
// below) and volume track (the hit area; the drawn track is thinner).
type flowColumn struct {
	caption, field, track rect
}

func (o flowOutputsRow) columns(c *canvas, r rect) [2]flowColumn {
	lc, rc := flowCols(c, r)
	var out [2]flowColumn
	for i, col := range []rect{lc, rc} {
		f := rect{col.Left, r.Top + c.px(20), col.Right, r.Top + c.px(20+flowFieldL)}
		_, bar := dockedUnit(c, f)
		out[i] = flowColumn{
			caption: rect{col.Left, r.Top + c.px(2), col.Right, r.Top + c.px(18)},
			field:   f,
			track:   rect{col.Left, bar.Bottom + c.px(4), col.Right, bar.Bottom + c.px(24)},
		}
	}
	return out
}

// cardPadL is the cable card's inner padding.
const cardPadL = 10

// infoBox is the cable card's info icon, right after its mic icon.
func (o flowOutputsRow) infoBox(c *canvas, card rect) rect {
	left := card.Left + c.px(cardPadL) + c.px(16) + c.px(1)
	return rect{left, card.Top + c.px(6), left + c.px(15), card.Bottom - c.px(6)}
}

func (o flowOutputsRow) volumes() [2]*FlowVolume { return [2]*FlowVolume{o.cable.Volume, o.you.Volume} }

// Segments: the cable card, its info icon, the you field, then the two
// volume tracks.
const (
	segCableCard = iota
	segCableInfo
	segYouField
	segCableVolume
	segYouVolume
)

func (o flowOutputsRow) segments(c *canvas, r rect) []rect {
	cols := o.columns(c, r)
	segs := []rect{cols[0].field, o.infoBox(c, cols[0].field), cols[1].field, cols[0].track, cols[1].track}
	if o.cable.OnClick == nil && o.cable.Tip == "" {
		segs[segCableCard] = rect{}
	}
	if o.cable.Info == "" {
		segs[segCableInfo] = rect{}
	}
	for i, v := range o.volumes() {
		if v == nil {
			segs[segCableVolume+i] = rect{}
		}
	}
	return segs
}

func (o flowOutputsRow) click(seg int) {
	if seg == segCableCard && o.cable.OnClick != nil {
		o.cable.OnClick()
	}
}
func (flowOutputsRow) keepsOpen() bool { return true }

func (o flowOutputsRow) field(seg int) (dropdownField, bool) {
	return o.you.field(), seg == segYouField
}

// dragIDs of the two volume tracks.
var flowVolumeIDs = [2]string{"flow-volume:cable", "flow-volume:you"}

func (o flowOutputsRow) dragIDAt(seg int) string {
	i := seg - segCableVolume
	if i < 0 || i > 1 || o.volumes()[i] == nil {
		return ""
	}
	return flowVolumeIDs[i]
}

// trackRange returns the drawn track and where its knob sits at 0 and 100.
func trackRange(c *canvas, hit rect) (track rect, x0, x1 int32) {
	h := c.px(12)
	top := hit.Top + (hit.h()-h)/2
	track = rect{hit.Left, top, hit.Right, top + h}
	half := c.px(sliderKnobWL) / 2
	return track, track.Left + half, track.Right - half
}

func (o flowOutputsRow) dragTo(c *canvas, r rect, id string, x int32, done bool) {
	cols := o.columns(c, r)
	for i, v := range o.volumes() {
		if v == nil || flowVolumeIDs[i] != id {
			continue
		}
		_, x0, x1 := trackRange(c, cols[i].track)
		val := v.Value
		if x1 > x0 {
			val = max(0, min(100, int((int64(x-x0)*100+int64(x1-x0)/2)/int64(x1-x0))))
		}
		if v.OnChange != nil && (val != v.Value || done) {
			v.OnChange(val, done)
		}
	}
}

func (o flowOutputsRow) tip(c *canvas, r rect, seg int) string {
	switch seg {
	case segCableCard:
		return o.cable.Tip
	case segCableInfo:
		return o.cable.Info
	case segYouField:
		return fieldTip(c, o.columns(c, r)[1].field, o.you.field())
	case segCableVolume, segYouVolume:
		if v := o.volumes()[seg-segCableVolume]; v != nil {
			return v.Tip
		}
	}
	return ""
}

func (o flowOutputsRow) paint(c *canvas, r rect, st rowState) {
	cols := o.columns(c, r)

	// The cable's far end: a card, not a choice.
	col := cols[0]
	paintCaption(c, col.caption, o.cable.Caption, o.cable.Value)
	bg := fieldBg(false, o.cable.OnClick != nil && st.hover == segCableCard, o.cable.OnClick != nil && st.press == segCableCard)
	paintDocked(c, col.field, bg, readLevel(o.cable.Level))
	card := col.field
	pad := c.px(cardPadL)
	name, fg := o.cable.Name, colText
	if name == "" {
		name, fg = o.cable.Placeholder, colMuted
	}
	c.icon(IconMic, rect{card.Left + pad, card.Top, card.Left + pad + c.px(16), card.Bottom}, 16, fg)
	textLeft := card.Left + pad + c.px(16) + c.px(7)
	if o.cable.Info != "" {
		info := o.infoBox(c, card)
		infoCol := colMuted
		if st.hover == segCableInfo {
			infoCol = colText
		}
		c.icon(IconInfo, info, 12, infoCol)
		textLeft = info.Right + c.px(6)
	}
	c.text(name, rect{textLeft, card.Top, card.Right - pad, card.Bottom}, fontBold, fg, dtLeft)

	// The user's own output: a choice.
	col = cols[1]
	paintCaption(c, col.caption, o.you.Caption, o.you.Value)
	paintDockedField(c, col.field, o.you.field(), readLevel(o.you.Level), st.open == segYouField,
		st.hover == segYouField, st.press == segYouField)

	for i, v := range o.volumes() {
		if v != nil {
			track, x0, x1 := trackRange(c, cols[i].track)
			paintTrack(c, track, track.h(), x0, x1, v.Value, 100, st.hover == segCableVolume+i, st.press == segCableVolume+i)
		}
	}
}
