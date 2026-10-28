package ui

import "strconv"

// The ducking slider: what holding push-to-talk yourself does to the
// sounds, at the bottom of the push-to-talk card (KeybindSpec.Duck). A
// caption, "Ducking" with an info icon, over a full-width rounded track
// with a rectangular knob to drag. All the way left the knob says "Pause"
// (the sounds pause); further right they keep playing, louder the further
// it goes, the knob saying how loud, up to 100% at the right end. The
// track fills up to the knob in a tint of the accent, a pause icon at its
// left end and a speaker at its right one, under the knob when it is there.

// DuckSpec describes the ducking slider of a push-to-talk card.
type DuckSpec struct {
	Value     int    // 0: pause; 1–100: the sounds' volume while the key is held
	Inherited bool   // the global value, in an app's card: muted, "Global" beside it
	Info      string // the info icon's tooltip
	// OnChange gets every new value while the knob is dragged (done false)
	// and the last one on release (done true).
	OnChange func(value int, done bool)
}

// Logical sizes.
const (
	duckCaptionL = 24 // "Ducking" and the info icon
	duckTrackL   = 34
	duckRadiusL  = 9
	duckKnobWL   = 72
	duckInsetL   = 3 // between the track and the knob
	duckIconL    = 13
	duckStep     = 5 // the knob snaps to these
)

// duckLayout is where the slider's parts go, in device pixels.
type duckLayout struct {
	label, info, scope, track rect
}

// layoutDuck lays the slider out from top between left and right, and
// returns its bottom.
func layoutDuck(c *canvas, s DuckSpec, left, right, top int32) (duckLayout, int32) {
	var l duckLayout
	l.label = rect{left, top, left + c.textWidth("Ducking", fontBody) + c.px(2), top + c.px(duckCaptionL)}
	size := c.px(20)
	y := top + (c.px(duckCaptionL)-size)/2
	l.info = rect{l.label.Right + c.px(1), y, l.label.Right + c.px(1) + size, y + size}
	if s.Inherited {
		l.scope = rect{right - c.textWidth("GLOBAL", fontCaption), top, right, top + c.px(duckCaptionL)}
	}
	trackTop := l.label.Bottom + c.px(4)
	l.track = rect{left, trackTop, right, trackTop + c.px(duckTrackL)}
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
	fg := colText
	if s.Inherited {
		fg = colMuted
	}
	c.text("Ducking", l.label, fontBody, fg, dtLeft)
	infoCol := colCaption
	if hoverInfo {
		infoCol = colText
	}
	c.icon(IconInfo, l.info, 14, infoCol)
	if l.scope != (rect{}) {
		c.text("GLOBAL", l.scope, fontCaption, colCaption, dtRight)
	}

	radius := c.px(duckRadiusL)
	c.roundRect(l.track, radius, shade(bg, -14))
	knob := duckKnob(c, l.track, s.Value)
	if s.Value > 0 { // the sounds' volume: the track fills up to the knob
		fill := rect{l.track.Left, l.track.Top, (knob.Left + knob.Right) / 2, l.track.Bottom}
		c.roundRect(fill, radius, mix(shade(bg, -14), colAccent, 0.2))
	}
	pad, icon := c.px(10), c.px(duckIconL)
	mid := (l.track.Top + l.track.Bottom) / 2
	c.icon(IconPause, rect{l.track.Left + pad, mid - icon/2, l.track.Left + pad + icon, mid + icon/2}, duckIconL, colCaption)
	c.icon(IconVolume, rect{l.track.Right - pad - icon, mid - icon/2, l.track.Right - pad, mid + icon/2}, duckIconL, colCaption)

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
}
