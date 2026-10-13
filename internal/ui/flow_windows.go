package ui

// The audio flow: a small diagram of where audio goes, built from four rows
// stacked like any others. At the top the microphone (full width) and the
// soundboard (right column). The soundboard plays two streams at once: one
// into VB-Cable, one to the user's headphones. The cable is drawn as a
// cable: the microphone (through Windows' "Listen to this device") and the
// soundboard's cable stream plug into its input end; its far end is "CABLE
// Output", the microphone others' apps listen to.
//
//	YOUR MICROPHONE
//	[ Headset mic                    v ]
//	[▮▮▮▮▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯]
//	 │ (mic badge)       SOUNDBOARD
//	 │                   [▶ Test sound   ]
//	 │                   [▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯]
//	 │                    │             │
//	┌┴┐───(cable badge)───┘             │
//	 ┃ VB-Cable                         │
//	 ┃ (cable badge)       (you badge)  │
//	└┬┘                                 v
//	OTHERS HEAR   100%   YOU HEAR     80%
//	[🎤 CABLE Output ]   [Speakers      v]
//	[▮▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯]   [▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯]
//	[=======|        ]   [=====|         ]
//	Your mic in apps
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
// name others' apps know it by, e.g. "CABLE Output", a level bar, the
// volume of the sounds sent into it, and a hint where to pick it.
type FlowCable struct {
	Caption, Value string
	Name           string // "" shows Placeholder, muted
	Placeholder    string
	Hint           string // under the outputs (the right column has none)
	Tip            string // the card's tooltip
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

	MicPipe   Pipe // microphone → the cable's input ("Listen to this device")
	CablePipe Pipe // soundboard → the cable's input
	CableLine Pipe // the cable itself: mic and sounds together, to CABLE Output
	YouPipe   Pipe // soundboard → you hear
}

// Rows returns the flow's rows: the microphone, the soundboard, the
// junction with the cable, and the two outputs.
func (f AudioFlow) Rows() []Row {
	return []Row{
		flowMicRow{f.Mic, f.MicPipe},
		flowSoundsRow{f.Sounds, f.MicPipe, f.CablePipe, f.YouPipe},
		flowJunctionRow{f.MicPipe, f.CablePipe, f.CableLine, f.YouPipe},
		flowOutputsRow{f.Cable, f.You},
	}
}

// --- geometry and painting shared by the rows ----------------------------------

// Logical sizes.
const (
	flowColGapL    = 12 // between the two columns
	flowLaneInL    = 18 // a lane's distance from its column's edge
	flowPipeL      = 2  // pipe width
	flowCableL     = 4  // the cable's width
	flowFieldL     = 34 // dropdowns, the cable card and the test button
	flowMeterL     = 5  // level bar height
	flowBadgeL     = 20 // badge height
	flowBadgeIconL = 14
)

// flowCols returns the left and right column of a row.
func flowCols(c *canvas, r rect) (left, right rect) {
	in := inset(c, r)
	w := (in.w() - c.px(flowColGapL)) / 2
	return rect{in.Left, r.Top, in.Left + w, r.Bottom}, rect{in.Right - w, r.Top, in.Right, r.Bottom}
}

// flowLanes returns the x of the three lanes: the microphone and the cable
// (left), the soundboard's stream into the cable (right column, inner) and
// its stream to the headphones (right column, further right, where its
// arrow lands between the output's caption and its volume).
func flowLanes(c *canvas, r rect) (mic, toCable, toYou int32) {
	lc, rc := flowCols(c, r)
	return lc.Left + c.px(flowLaneInL), rc.Left + c.px(flowLaneInL), rc.Left + rc.w()*5/8
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

// pipeV draws a vertical pipe of width wL (logical) from y0 to y1 at x.
func pipeV(c *canvas, x, y0, y1 int32, p Pipe, wL int) {
	col, dashed := pipeColor(p)
	w := c.px(wL)
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
	w := c.px(flowPipeL)
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
func vGapped(c *canvas, x, y0, y1 int32, badge rect, p Pipe, wL int) {
	if badge.h() == 0 {
		pipeV(c, x, y0, y1, p, wL)
		return
	}
	pipeV(c, x, y0, badge.Top, p, wL)
	pipeV(c, x, badge.Bottom, y1, p, wL)
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
// (for the outer right lane). paintBadge puts the icon on the right then.
func badgeOnLaneLeft(c *canvas, b *Badge, x, cy, minLeft int32) rect {
	h := c.px(flowBadgeL)
	right := x + c.px(flowBadgeIconL)/2 + c.px(6)
	return rect{max(right-badgeWidth(c, b), minLeft), cy - h/2, right, cy - h/2 + h}
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

// paintPlug draws one end of the cable centred on (cx, cy): a rounded body
// with two prongs pointing up (the input end) or down (the output end).
func paintPlug(c *canvas, cx, cy int32, up bool, col uint32) {
	w, h := c.px(16), c.px(10)
	body := rect{cx - w/2, cy - h/2, cx - w/2 + w, cy - h/2 + h}
	c.roundRect(body, c.px(3), col)
	pw, pl, off := max(1, c.px(2)), c.px(4), c.px(4)
	for _, x := range []int32{cx - off - pw/2, cx + off - pw/2} {
		if up {
			c.fill(rect{x, body.Top - pl, x + pw, body.Top}, col)
		} else {
			c.fill(rect{x, body.Bottom, x + pw, body.Bottom + pl}, col)
		}
	}
}

// paintMeter draws a level bar: small blocks like Windows' own meters,
// green, yellow over the last stretch, red at the top.
func paintMeter(c *canvas, box rect, level float64) {
	c.roundRect(box, box.h(), colControl)
	if level <= 0 {
		return
	}
	bw, gap := max(2, c.px(3)), max(1, c.px(1))
	end := box.Left + int32(float64(box.w())*min(1, level))
	for x := box.Left; x < end; x += bw + gap {
		col := colAccent
		switch f := float64(x-box.Left) / float64(box.w()); {
		case f >= 0.98:
			col = colDanger
		case f >= 0.89:
			col = colWarning
		}
		c.fill(rect{x, box.Top, min(x+bw, end), box.Bottom}, col)
	}
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

type flowMicRow struct {
	dev  FlowDevice
	pipe Pipe
}

func (flowMicRow) height() int { return 78 }

func (m flowMicRow) geometry(c *canvas, r rect) (caption, field, meter rect) {
	in := inset(c, r)
	caption = rect{in.Left, r.Top + c.px(2), in.Right, r.Top + c.px(20)}
	field = rect{in.Left, r.Top + c.px(22), in.Right, r.Top + c.px(22+flowFieldL)}
	meter = rect{in.Left, field.Bottom + c.px(6), in.Right, field.Bottom + c.px(6+flowMeterL)}
	return
}

func (m flowMicRow) segments(c *canvas, r rect) []rect {
	_, field, _ := m.geometry(c, r)
	return []rect{field}
}
func (flowMicRow) click(int)       {}
func (flowMicRow) keepsOpen() bool { return true }

func (m flowMicRow) field(seg int) (dropdownField, bool) { return m.dev.field(), seg == 0 }

func (m flowMicRow) tip(c *canvas, r rect, _ int) string {
	_, field, _ := m.geometry(c, r)
	return fieldTip(c, field, m.dev.field())
}

func (m flowMicRow) paint(c *canvas, r rect, st rowState) {
	caption, field, meter := m.geometry(c, r)
	paintCaption(c, caption, m.dev.Caption, m.dev.Value)
	paintField(c, field, m.dev.field(), st.open == 0, st.hover == 0, st.press == 0)
	paintMeter(c, meter, readLevel(m.dev.Level))
	lane, _, _ := flowLanes(c, r)
	pipeV(c, lane, meter.Bottom+c.px(4), r.Bottom, m.pipe, flowPipeL)
}

// --- soundboard ----------------------------------------------------------------

type flowSoundsRow struct {
	s                 FlowSounds
	mic, toCable, you Pipe
}

func (flowSoundsRow) height() int { return 86 }

func (s flowSoundsRow) geometry(c *canvas, r rect) (caption, button, meter, badge rect) {
	_, rc := flowCols(c, r)
	caption = rect{rc.Left, r.Top + c.px(4), rc.Right, r.Top + c.px(20)}
	button = rect{rc.Left, r.Top + c.px(22), rc.Right, r.Top + c.px(22+flowFieldL)}
	meter = rect{rc.Left, button.Bottom + c.px(6), rc.Right, button.Bottom + c.px(6+flowMeterL)}
	if s.mic.Badge != nil {
		lane, _, _ := flowLanes(c, r)
		badge = badgeOnLane(c, s.mic.Badge, lane, button.Top+button.h()/2, rc.Left-c.px(8))
	}
	return
}

func (s flowSoundsRow) segments(c *canvas, r rect) []rect {
	_, button, _, badge := s.geometry(c, r)
	return []rect{button, badge}
}

func (s flowSoundsRow) click(seg int) {
	switch {
	case seg == 0 && s.s.Playing && s.s.OnStop != nil:
		s.s.OnStop()
	case seg == 0 && !s.s.Playing && s.s.OnTest != nil:
		s.s.OnTest()
	case seg == 1:
		clickBadge(s.mic.Badge)
	}
}
func (flowSoundsRow) keepsOpen() bool { return true }

func (s flowSoundsRow) tip(_ *canvas, _ rect, seg int) string {
	if seg == 1 {
		return badgeTip(s.mic.Badge)
	}
	return ""
}

func (s flowSoundsRow) paint(c *canvas, r rect, st rowState) {
	caption, button, meter, badge := s.geometry(c, r)
	paintCaption(c, caption, s.s.Caption, s.s.Status)
	label, icon := "Test sound", IconPlay
	if s.s.Playing {
		label, icon = "Stop", IconStop
	}
	paintButton(c, button, label, icon, ButtonNormal, false, st.hover == 0, st.press == 0)
	paintMeter(c, meter, readLevel(s.s.Level))

	mic, toCable, toYou := flowLanes(c, r)
	vGapped(c, mic, r.Top, r.Bottom, badge, s.mic, flowPipeL)
	if s.mic.Badge != nil {
		paintBadge(c, badge, s.mic.Badge, st.hover == 1, st.press == 1, false)
	}
	pipeV(c, toCable, meter.Bottom+c.px(4), r.Bottom, s.toCable, flowPipeL)
	pipeV(c, toYou, meter.Bottom+c.px(4), r.Bottom, s.you, flowPipeL)
}

// --- junction: the cable --------------------------------------------------------------

type flowJunctionRow struct {
	mic, toCable, cable, you Pipe
}

func (flowJunctionRow) height() int { return 100 }

// ys returns the y of the input plug, of the cable's label and badge, and of
// the output plug.
func (flowJunctionRow) ys(c *canvas, r rect) (inPlug, label, badge, outPlug int32) {
	return r.Top + c.px(26), r.Top + c.px(48), r.Top + c.px(70), r.Bottom - c.px(10)
}

// badges returns the boxes of the cable-stream, cable and you badges (empty
// where a pipe has none).
func (j flowJunctionRow) badges(c *canvas, r rect) [3]rect {
	var out [3]rect
	mic, toCable, toYou := flowLanes(c, r)
	inPlug, _, badge, _ := j.ys(c, r)
	if b := j.toCable.Badge; b != nil {
		w := min(badgeWidth(c, b), toCable-mic-c.px(24))
		mid := (mic + c.px(8) + toCable) / 2
		h := c.px(flowBadgeL)
		out[0] = rect{mid - w/2, inPlug - h/2, mid - w/2 + w, inPlug - h/2 + h}
	}
	if b := j.cable.Badge; b != nil {
		out[1] = badgeOnLane(c, b, mic, badge, toCable-c.px(4))
	}
	if b := j.you.Badge; b != nil {
		minLeft := mic + c.px(16)
		if out[1].w() > 0 {
			minLeft = out[1].Right + c.px(6)
		}
		out[2] = badgeOnLaneLeft(c, b, toYou, badge, minLeft)
	}
	return out
}

func (j flowJunctionRow) pipes() [3]Pipe { return [3]Pipe{j.toCable, j.cable, j.you} }

func (j flowJunctionRow) segments(c *canvas, r rect) []rect {
	b := j.badges(c, r)
	return b[:]
}

func (j flowJunctionRow) click(seg int) {
	if seg >= 0 && seg < 3 {
		clickBadge(j.pipes()[seg].Badge)
	}
}
func (flowJunctionRow) keepsOpen() bool { return true }

func (j flowJunctionRow) tip(_ *canvas, _ rect, seg int) string {
	if seg >= 0 && seg < 3 {
		return badgeTip(j.pipes()[seg].Badge)
	}
	return ""
}

func (j flowJunctionRow) paint(c *canvas, r rect, st rowState) {
	mic, toCable, toYou := flowLanes(c, r)
	inPlug, label, _, outPlug := j.ys(c, r)
	boxes := j.badges(c, r)
	plugH := c.px(10)
	cableCol, _ := pipeColor(j.cable)

	// The mic comes down into the cable's input plug ...
	pipeV(c, mic, r.Top, inPlug-plugH/2-c.px(4), j.mic, flowPipeL)
	// ... and so does the soundboard's cable stream, from the right.
	pipeV(c, toCable, r.Top, inPlug+c.px(1), j.toCable, flowPipeL)
	if b := boxes[0]; b.w() > 0 {
		pipeH(c, mic+c.px(8), b.Left, inPlug, j.toCable)
		pipeH(c, b.Right, toCable+c.px(1), inPlug, j.toCable)
	} else {
		pipeH(c, mic+c.px(8), toCable+c.px(1), inPlug, j.toCable)
	}
	// The cable, between its two plugs.
	vGapped(c, mic, inPlug+plugH/2, outPlug-plugH/2, boxes[1], j.cable, flowCableL)
	paintPlug(c, mic, inPlug, true, cableCol)
	paintPlug(c, mic, outPlug, false, cableCol)
	if j.cable.Badge == nil || boxes[1].Top > label+c.px(10) {
		c.text("VB-Cable", rect{mic + c.px(12), label - c.px(9), toCable - c.px(4), label + c.px(9)}, fontCaption, colCaption, dtLeft)
	}

	// The headphones' stream, straight down.
	tip := r.Bottom - c.px(3)
	vGapped(c, toYou, r.Top, tip-c.px(2), boxes[2], j.you, flowPipeL)
	youCol, _ := pipeColor(j.you)
	c.chevron(toYou, tip-c.px(2), c.px(4), false, youCol)

	for i, p := range j.pipes() {
		if p.Badge != nil {
			paintBadge(c, boxes[i], p.Badge, st.hover == i, st.press == i, i == 2)
		}
	}
}

// --- outputs ---------------------------------------------------------------------

type flowOutputsRow struct {
	cable FlowCable
	you   FlowDevice
}

func (flowOutputsRow) height() int { return 114 }

// flowColumn is one output's caption, card or field, meter, volume track
// (the hit area; the drawn track is thinner) and hint.
type flowColumn struct {
	caption, field, meter, track, hint rect
}

func (o flowOutputsRow) columns(c *canvas, r rect) [2]flowColumn {
	lc, rc := flowCols(c, r)
	var out [2]flowColumn
	for i, col := range []rect{lc, rc} {
		f := rect{col.Left, r.Top + c.px(20), col.Right, r.Top + c.px(20+flowFieldL)}
		m := rect{col.Left, f.Bottom + c.px(6), col.Right, f.Bottom + c.px(6+flowMeterL)}
		t := rect{col.Left, m.Bottom + c.px(4), col.Right, m.Bottom + c.px(24)}
		out[i] = flowColumn{
			caption: rect{col.Left, r.Top + c.px(2), col.Right, r.Top + c.px(18)},
			field:   f,
			meter:   m,
			track:   t,
			hint:    rect{col.Left, t.Bottom, col.Right, t.Bottom + c.px(16)},
		}
	}
	return out
}

func (o flowOutputsRow) volumes() [2]*FlowVolume { return [2]*FlowVolume{o.cable.Volume, o.you.Volume} }

// segments: the cable card, the you field, then the two volume tracks.
func (o flowOutputsRow) segments(c *canvas, r rect) []rect {
	cols := o.columns(c, r)
	segs := []rect{cols[0].field, cols[1].field, cols[0].track, cols[1].track}
	if o.cable.OnClick == nil && o.cable.Tip == "" {
		segs[0] = rect{}
	}
	for i, v := range o.volumes() {
		if v == nil {
			segs[2+i] = rect{}
		}
	}
	return segs
}

func (o flowOutputsRow) click(seg int) {
	if seg == 0 && o.cable.OnClick != nil {
		o.cable.OnClick()
	}
}
func (flowOutputsRow) keepsOpen() bool { return true }

func (o flowOutputsRow) field(seg int) (dropdownField, bool) { return o.you.field(), seg == 1 }

// dragIDs of the two volume tracks.
var flowVolumeIDs = [2]string{"flow-volume:cable", "flow-volume:you"}

func (o flowOutputsRow) dragIDAt(seg int) string {
	if seg < 2 || seg > 3 || o.volumes()[seg-2] == nil {
		return ""
	}
	return flowVolumeIDs[seg-2]
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
	case 0:
		return o.cable.Tip
	case 1:
		return fieldTip(c, o.columns(c, r)[1].field, o.you.field())
	case 2, 3:
		if v := o.volumes()[seg-2]; v != nil {
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
	bg := colRowHover
	if o.cable.OnClick != nil {
		switch {
		case st.press == 0:
			bg = colRowPress
		case st.hover == 0:
			bg = shade(colRowHover, 10)
		}
	}
	c.roundRect(col.field, c.px(10), bg)
	pad := c.px(12)
	icon := rect{col.field.Left + pad, col.field.Top, col.field.Left + pad + c.px(16), col.field.Bottom}
	name, fg := o.cable.Name, colText
	if name == "" {
		name, fg = o.cable.Placeholder, colMuted
	}
	c.icon(IconMic, icon, 16, fg)
	c.text(name, rect{icon.Right + c.px(8), col.field.Top, col.field.Right - pad, col.field.Bottom}, fontBold, fg, dtLeft)
	paintMeter(c, col.meter, readLevel(o.cable.Level))
	if o.cable.Hint != "" { // across both columns: "You hear" has none
		c.text(o.cable.Hint, rect{col.hint.Left, col.hint.Top, cols[1].hint.Right, col.hint.Bottom}, fontCaption, colMuted, dtLeft)
	}

	// The user's own output: a choice.
	col = cols[1]
	paintCaption(c, col.caption, o.you.Caption, o.you.Value)
	paintField(c, col.field, o.you.field(), st.open == 1, st.hover == 1, st.press == 1)
	paintMeter(c, col.meter, readLevel(o.you.Level))

	for i, v := range o.volumes() {
		if v != nil {
			track, x0, x1 := trackRange(c, cols[i].track)
			paintTrack(c, track, track.h(), x0, x1, v.Value, 100, st.hover == 2+i, st.press == 2+i)
		}
	}
}
