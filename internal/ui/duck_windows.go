package ui

import "strconv"

// The ducking slider: what holding push-to-talk yourself does to the
// sounds, at the bottom of the push-to-talk card (KeybindSpec.Duck). A
// full-width rounded track with a rectangular knob to drag. All the way left
// the knob says "Pause" (the sounds pause), a small triangle beside it
// pointing the way it pulls; further right they keep playing, louder the
// further it goes, the knob saying how loud, up to 100% at the right end.
// The track fills up to the knob's right edge in a tint of the accent, with
// a pause icon at its left end and a speaker at its right one. Left of the
// speaker, an info icon and "Ducking" in light grey, which give way to the
// knob once it is pulled past half way.

// DuckSpec describes the ducking slider of a push-to-talk card.
type DuckSpec struct {
	Value     int    // 0: pause; 1–100: the sounds' volume while the key is held
	Inherited bool   // the global value, in an app's card: muted, labelled so
	Info      string // the info icon's tooltip
	// OnChange gets every new value while the knob is dragged (done false)
	// and the last one on release (done true).
	OnChange func(value int, done bool)
}

// Logical sizes.
const (
	duckTrackL   = 27
	duckRadiusL  = 8
	duckKnobWL   = 72
	duckInsetL   = 3 // between the track and the knob
	duckIconL    = 13
	duckPadL     = 10 // the icons from the track's ends
	duckLabelMax = 50 // the label shows up to this value
	duckStep     = 5  // the knob snaps to these
)

// duckLayout is where the slider's parts go, in device pixels; label and
// info are empty while the knob is past duckLabelMax.
type duckLayout struct {
	track, speaker, label, info rect
}

// duckLabel is the label on the track.
func duckLabel(s DuckSpec) string {
	if s.Inherited {
		return "Ducking (global)"
	}
	return "Ducking"
}

// layoutDuck lays the slider out from top between left and right, and
// returns its bottom.
func layoutDuck(c *canvas, s DuckSpec, left, right, top int32) (duckLayout, int32) {
	var l duckLayout
	l.track = rect{left, top, right, top + c.px(duckTrackL)}
	mid := (l.track.Top + l.track.Bottom) / 2
	// From the right: the speaker at the end, the label, its info icon.
	x, icon := l.track.Right-c.px(duckPadL), c.px(duckIconL)
	l.speaker = rect{x - icon, mid - icon/2, x, mid + icon/2}
	if s.Value <= duckLabelMax {
		w := c.textWidth(duckLabel(s), fontBody) + c.px(2)
		l.label = rect{l.speaker.Left - c.px(8) - w, l.track.Top, l.speaker.Left - c.px(8), l.track.Bottom}
		size := c.px(18)
		l.info = rect{l.label.Left - c.px(1) - size, mid - size/2, l.label.Left - c.px(1), mid + size/2}
	}
	return l, l.track.Bottom
}

// duckRange is where the knob's centre is at 0 and at 100.
func duckRange(c *canvas, track rect) (x0, x1 int32) {
	half := c.px(duckInsetL) + c.px(duckKnobWL)/2
	return track.Left + half, track.Right - half
}

func duckKnob(c *canvas, track rect, value int) rect {
	x0, x1 := duckRange(c, track)
	kx := x0 + int32(int64(x1-x0)*int64(max(0, min(100, value)))/100)
	in := c.px(duckInsetL)
	w := c.px(duckKnobWL)
	return rect{kx - w/2, track.Top + in, kx - w/2 + w, track.Bottom - in}
}

// duckValueAt is the value for the knob's centre at x, snapped to
// duckStep.
func duckValueAt(c *canvas, track rect, x int32) int {
	x0, x1 := duckRange(c, track)
	if x1 <= x0 {
		return 0
	}
	v := float64(x-x0) * 100 / float64(x1-x0)
	step := float64(duckStep)
	return max(0, min(100, int(v/step+0.5)*duckStep))
}

// DuckText is what the knob says for a value: "Pause", else the volume.
func DuckText(value int) string {
	if value <= 0 {
		return "Pause"
	}
	return strconv.Itoa(value) + "%"
}

// paintDuck draws the slider on a card of colour bg.
func paintDuck(c *canvas, l duckLayout, s DuckSpec, bg uint32, hoverInfo, hover, press bool) {
	radius := c.px(duckRadiusL)
	trackBg := shade(bg, -14)
	c.roundRect(l.track, radius, trackBg)
	knob := duckKnob(c, l.track, s.Value)
	if s.Value > 0 { // the sounds' volume: the track fills up to the knob's right edge
		fill := rect{l.track.Left, l.track.Top, knob.Right + c.px(duckInsetL), l.track.Bottom}
		c.roundRect(fill, radius, mix(trackBg, colAccent, 0.2))
	}
	pad, icon := c.px(duckPadL), c.px(duckIconL)
	mid := (l.track.Top + l.track.Bottom) / 2
	c.icon(IconPause, rect{l.track.Left + pad, mid - icon/2, l.track.Left + pad + icon, mid + icon/2}, duckIconL, colCaption)
	c.icon(IconVolume, l.speaker, duckIconL, colCaption)
	if l.label != (rect{}) {
		c.text(duckLabel(s), l.label, fontBody, colMuted, dtLeft)
		infoCol := colCaption
		if hoverInfo {
			infoCol = colText
		}
		c.icon(IconInfo, l.info, 14, infoCol)
	}

	knobBg, knobFg := colControl, colText
	switch {
	case press:
		knobBg, knobFg = colAccent, colAccentText
	case hover:
		knobBg = shade(colControl, 14)
	case s.Inherited:
		knobFg = colMuted
	}
	c.roundRect(knob, radius-c.px(duckInsetL), knobBg)
	c.text(DuckText(s.Value), knob, fontBold, knobFg, dtCenter)
	if s.Value == 0 { // pull me: a small triangle pointing right, beside the knob
		col := colMuted
		if hover || press {
			col = colText
		}
		paintTriangleRight(c, knob.Right+c.px(7), mid, c.px(4), col)
	}
}

// paintTriangleRight fills a small triangle pointing right, its left edge
// at x, centred on y, half as high as it is wide times two.
func paintTriangleRight(c *canvas, x, y, half int32, color uint32) {
	for dy := -half; dy <= half; dy++ {
		w := half - abs32i(dy) + 1
		c.fill(rect{x, y + dy, x + w, y + dy + 1}, color)
	}
}

func abs32i(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
