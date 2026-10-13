package ui

// The audio flow: a small diagram of where audio goes, built from four rows
// stacked like any others. At the top the microphone (full width) and the
// soundboard (right column); pipes lead down into the two outputs side by
// side: "others hear" (the virtual cable) on the left, "you hear" on the
// right. The mic's pipe runs straight down the left; the soundboard's runs
// down the right and branches left into the cable.
//
//	MICROPHONE
//	[ Headset mic                v ]
//	[▮▮▮▮▮▮▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯]
//	 │                 SOUNDBOARD
//	 │ (mic badge)     [▶ Test sound ]
//	 │                 [▮▮▯▯▯▯▯▯▯▯▯▯]
//	 │                  │ (sounds badge)
//	 ●────(cable badge)─┤
//	 │ (others badge)   │ (you badge)
//	 v                  v
//	OTHERS HEAR  100%  YOU HEAR    80%
//	[CABLE Input   v]  [Speakers   v]
//	[▮▮▮▯▯▯▯▯▯▯▯▯▯▯]  [▮▮▯▯▯▯▯▯▯▯▯▯▯]
//	[=======|      ]  [=====|        ]
//
// The rows share the column and lane geometry, so the pipes join across row
// boundaries. Pipes light up with the signal passing through them, read on
// every paint (Popup.Repaint animates them). A pipe that should carry audio
// but doesn't is drawn broken, with a badge on the gap that explains why
// (tooltip) and leads to the fix (click).

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

// AudioFlow is the whole diagram. Rows returns its rows, to be placed on a
// page like any others.
type AudioFlow struct {
	Mic    FlowDevice
	Sounds FlowSounds
	Others FlowDevice // the virtual cable: what others hear
	You    FlowDevice // the user's speakers or headphones

	MicPipe    Pipe // microphone → others hear ("Listen to this device")
	SoundsPipe Pipe // soundboard → the branch
	CablePipe  Pipe // the branch → others hear (Resound's cable output)
	OthersPipe Pipe // the merged mic and sounds → others hear
	YouPipe    Pipe // the branch → you hear
}

// Rows returns the flow's rows: the microphone, the soundboard, the
// junction and the two outputs.
func (f AudioFlow) Rows() []Row {
	return []Row{
		flowMicRow{f.Mic, f.MicPipe},
		flowSoundsRow{f.Sounds, f.MicPipe, f.SoundsPipe},
		flowJunctionRow{f.MicPipe, f.SoundsPipe, f.CablePipe, f.OthersPipe, f.YouPipe},
		flowOutputsRow{f.Others, f.You},
	}
}

// --- geometry and painting shared by the rows ----------------------------------

// Logical sizes.
const (
	flowColGapL    = 12 // between the two columns
	flowLaneInL    = 18 // a lane's distance from its column's left edge
	flowPipeL      = 2  // pipe width
	flowFieldL     = 34 // dropdowns and the test button
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

// flowLanes returns the x of the left (mic, others) and right (sounds,
// you) lane.
func flowLanes(c *canvas, r rect) (left, right int32) {
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
	w := c.px(flowPipeL)
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

// pipeVBadged draws a vertical pipe with its badge (if any) centred at cy:
// the line leaves a gap for the badge's icon, its label goes to the right
// up to maxRight. It returns the badge's box (empty without a badge).
func pipeVBadged(c *canvas, x, y0, y1, cy, maxRight int32, p Pipe, hover, press bool) rect {
	if p.Badge == nil {
		pipeV(c, x, y0, y1, p)
		return rect{}
	}
	box := badgeBox(c, p.Badge, x-c.px(flowBadgeIconL)/2-c.px(3), cy, maxRight)
	pipeV(c, x, y0, box.Top, p)
	pipeV(c, x, box.Bottom, y1, p)
	paintBadge(c, box, p.Badge, hover, press)
	return box
}

// badgeBox is the box of a badge whose left edge is at left, centred on cy
// and no wider than up to maxRight.
func badgeBox(c *canvas, b *Badge, left, cy, maxRight int32) rect {
	w := c.px(6) + c.px(flowBadgeIconL)
	if b.Label != "" {
		w += c.px(5) + c.textWidth(b.Label, fontCaption) + c.px(2)
	}
	w += c.px(6)
	h := c.px(flowBadgeL)
	return rect{left, cy - h/2, min(left+w, maxRight), cy - h/2 + h}
}

// paintBadge draws a badge: a pill in the panel's background colour (so it
// covers the pipe), the icon and the label in the badge's tone.
func paintBadge(c *canvas, box rect, b *Badge, hover, press bool) {
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
	ic := rect{box.Left + c.px(6), box.Top, box.Left + c.px(6) + c.px(flowBadgeIconL), box.Bottom}
	c.icon(b.Icon, ic, flowBadgeIconL, col)
	if b.Label != "" {
		c.text(b.Label, rect{ic.Right + c.px(5), box.Top, box.Right - c.px(6), box.Bottom}, fontCaption, col, dtLeft)
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
	lane, _ := flowLanes(c, r)
	pipeV(c, lane, meter.Bottom+c.px(4), r.Bottom, m.pipe)
}

// --- soundboard ----------------------------------------------------------------

type flowSoundsRow struct {
	s         FlowSounds
	mic, pipe Pipe
}

func (flowSoundsRow) height() int { return 86 }

func (s flowSoundsRow) geometry(c *canvas, r rect) (caption, button, meter, badge rect) {
	_, rc := flowCols(c, r)
	caption = rect{rc.Left, r.Top + c.px(4), rc.Right, r.Top + c.px(20)}
	button = rect{rc.Left, r.Top + c.px(22), rc.Right, r.Top + c.px(22+flowFieldL)}
	meter = rect{rc.Left, button.Bottom + c.px(6), rc.Right, button.Bottom + c.px(6+flowMeterL)}
	if s.mic.Badge != nil {
		lane, _ := flowLanes(c, r)
		badge = badgeBox(c, s.mic.Badge, lane-c.px(flowBadgeIconL)/2-c.px(3), button.Top+button.h()/2, rc.Left-c.px(8))
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
	caption, button, meter, _ := s.geometry(c, r)
	paintCaption(c, caption, s.s.Caption, s.s.Status)
	label, icon := "Test sound", IconPlay
	if s.s.Playing {
		label, icon = "Stop", IconStop
	}
	paintButton(c, button, label, icon, ButtonNormal, false, st.hover == 0, st.press == 0)
	paintMeter(c, meter, readLevel(s.s.Level))

	left, right := flowLanes(c, r)
	_, rc := flowCols(c, r)
	pipeVBadged(c, left, r.Top, r.Bottom, button.Top+button.h()/2, rc.Left-c.px(8), s.mic, st.hover == 1, st.press == 1)
	pipeV(c, right, meter.Bottom+c.px(4), r.Bottom, s.pipe)
}

// --- junction --------------------------------------------------------------------

type flowJunctionRow struct {
	mic, sounds, cable, others, you Pipe
}

func (flowJunctionRow) height() int { return 76 }

// ys returns the y of the upper badges, of the branch and of the lower badges.
func (flowJunctionRow) ys(c *canvas, r rect) (upper, branch, lower int32) {
	return r.Top + c.px(14), r.Top + c.px(34), r.Top + c.px(54)
}

// badges returns the boxes of the sounds, cable, others and you badges
// (empty where a pipe has none).
func (j flowJunctionRow) badges(c *canvas, r rect) [4]rect {
	var out [4]rect
	in := inset(c, r)
	left, right := flowLanes(c, r)
	_, rc := flowCols(c, r)
	upper, branch, lower := j.ys(c, r)
	iconOff := c.px(flowBadgeIconL)/2 + c.px(3)
	if b := j.sounds.Badge; b != nil {
		out[0] = badgeBox(c, b, right-iconOff, upper, in.Right)
	}
	if b := j.cable.Badge; b != nil {
		box := badgeBox(c, b, 0, branch, right-left-c.px(16))
		w := box.w()
		mid := (left + right) / 2
		out[1] = rect{mid - w/2, box.Top, mid - w/2 + w, box.Bottom}
	}
	if b := j.others.Badge; b != nil {
		out[2] = badgeBox(c, b, left-iconOff, lower, rc.Left-c.px(8))
	}
	if b := j.you.Badge; b != nil {
		out[3] = badgeBox(c, b, right-iconOff, lower, in.Right)
	}
	return out
}

func (j flowJunctionRow) pipes() [4]Pipe { return [4]Pipe{j.sounds, j.cable, j.others, j.you} }

func (j flowJunctionRow) segments(c *canvas, r rect) []rect {
	b := j.badges(c, r)
	return b[:]
}

func (j flowJunctionRow) click(seg int) {
	if seg >= 0 && seg < 4 {
		clickBadge(j.pipes()[seg].Badge)
	}
}
func (flowJunctionRow) keepsOpen() bool { return true }

func (j flowJunctionRow) tip(_ *canvas, _ rect, seg int) string {
	if seg >= 0 && seg < 4 {
		return badgeTip(j.pipes()[seg].Badge)
	}
	return ""
}

func (j flowJunctionRow) paint(c *canvas, r rect, st rowState) {
	left, right := flowLanes(c, r)
	_, branch, _ := j.ys(c, r)
	boxes := j.badges(c, r)
	tip := r.Bottom - c.px(3) // arrow tips, just above the outputs' captions

	// Left lane: the mic down to the merge, then mic and sounds together.
	pipeV(c, left, r.Top, branch, j.mic)
	vGapped(c, left, branch, tip-c.px(2), boxes[2], j.others)
	// Right lane: the sounds down to the branch, then on to the speakers.
	vGapped(c, right, r.Top, branch, boxes[0], j.sounds)
	vGapped(c, right, branch, tip-c.px(2), boxes[3], j.you)
	// The branch into the cable, leaving a gap for its badge.
	if b := boxes[1]; b.w() > 0 {
		pipeH(c, left, b.Left, branch, j.cable)
		pipeH(c, b.Right, right, branch, j.cable)
	} else {
		pipeH(c, left, right, branch, j.cable)
	}
	// Joints and arrows.
	dot := c.px(3)
	col, _ := pipeColor(j.others)
	c.circle(left, branch, dot, col)
	col, _ = pipeColor(j.sounds)
	c.circle(right, branch, dot, col)
	for _, a := range []struct {
		x int32
		p Pipe
	}{{left, j.others}, {right, j.you}} {
		col, _ := pipeColor(a.p)
		c.chevron(a.x, tip-c.px(2), c.px(4), false, col)
	}
	for i, p := range j.pipes() {
		if p.Badge != nil {
			paintBadge(c, boxes[i], p.Badge, st.hover == i, st.press == i)
		}
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

// --- outputs ---------------------------------------------------------------------

type flowOutputsRow struct {
	others, you FlowDevice
}

func (flowOutputsRow) height() int { return 98 }

// column is one output's caption, field, meter and volume track (the hit
// area; the drawn track is thinner).
type flowColumn struct {
	caption, field, meter, track rect
}

func (o flowOutputsRow) columns(c *canvas, r rect) [2]flowColumn {
	lc, rc := flowCols(c, r)
	var out [2]flowColumn
	for i, col := range []rect{lc, rc} {
		f := rect{col.Left, r.Top + c.px(20), col.Right, r.Top + c.px(20+flowFieldL)}
		m := rect{col.Left, f.Bottom + c.px(6), col.Right, f.Bottom + c.px(6+flowMeterL)}
		out[i] = flowColumn{
			caption: rect{col.Left, r.Top + c.px(2), col.Right, r.Top + c.px(18)},
			field:   f,
			meter:   m,
			track:   rect{col.Left, m.Bottom + c.px(4), col.Right, m.Bottom + c.px(24)},
		}
	}
	return out
}

func (o flowOutputsRow) devices() [2]FlowDevice { return [2]FlowDevice{o.others, o.you} }

// segments: the two fields, then the two volume tracks.
func (o flowOutputsRow) segments(c *canvas, r rect) []rect {
	cols := o.columns(c, r)
	segs := []rect{cols[0].field, cols[1].field, cols[0].track, cols[1].track}
	for i, d := range o.devices() {
		if d.Volume == nil {
			segs[2+i] = rect{}
		}
	}
	return segs
}

func (flowOutputsRow) click(int)       {} // fields open lists; tracks drag
func (flowOutputsRow) keepsOpen() bool { return true }

func (o flowOutputsRow) field(seg int) (dropdownField, bool) {
	if seg < 0 || seg > 1 {
		return dropdownField{}, false
	}
	return o.devices()[seg].field(), true
}

func (o flowOutputsRow) dragIDAt(seg int) string {
	if seg < 2 || seg > 3 || o.devices()[seg-2].Volume == nil {
		return ""
	}
	return "flow-volume:" + o.devices()[seg-2].ID
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
	for i, d := range o.devices() {
		if d.Volume == nil || "flow-volume:"+d.ID != id {
			continue
		}
		_, x0, x1 := trackRange(c, cols[i].track)
		v := d.Volume.Value
		if x1 > x0 {
			v = max(0, min(100, int((int64(x-x0)*100+int64(x1-x0)/2)/int64(x1-x0))))
		}
		if d.Volume.OnChange != nil && (v != d.Volume.Value || done) {
			d.Volume.OnChange(v, done)
		}
	}
}

func (o flowOutputsRow) tip(c *canvas, r rect, seg int) string {
	if seg < 0 || seg > 1 {
		return ""
	}
	return fieldTip(c, o.columns(c, r)[seg].field, o.devices()[seg].field())
}

func (o flowOutputsRow) paint(c *canvas, r rect, st rowState) {
	cols := o.columns(c, r)
	for i, d := range o.devices() {
		col := cols[i]
		paintCaption(c, col.caption, d.Caption, d.Value)
		paintField(c, col.field, d.field(), st.open == i, st.hover == i, st.press == i)
		paintMeter(c, col.meter, readLevel(d.Level))
		if d.Volume != nil {
			track, x0, x1 := trackRange(c, col.track)
			paintTrack(c, track, track.h(), x0, x1, d.Volume.Value, 100, st.hover == 2+i, st.press == 2+i)
		}
	}
}
