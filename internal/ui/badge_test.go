package ui

import "testing"

func TestParseBadgeAt(t *testing.T) {
	for _, at := range BadgeSpots {
		if ParseBadgeAt(at.String()) != at {
			t.Errorf("%s doesn't read back", at)
		}
	}
	for _, s := range []string{"", "middle", "Top-Left"} {
		if ParseBadgeAt(s) != BadgeTopCenter {
			t.Errorf("%q: want the default, top center", s)
		}
	}
}

// Slid in, the badge sits margin pixels from its edges (centred for the
// middle places); sliding, it moves towards its edge; nothing ever leaves
// the screen.
func TestBadgePlacement(t *testing.T) {
	mon := rect{1920, 0, 3840, 1080} // a screen right of another
	const w, h, pad, margin = 400, 120, 30, 20
	sw, sh := int32(w-2*pad), int32(h-2*pad)
	for _, at := range BadgeSpots {
		for _, p := range []float64{0, 0.5, 1} {
			dst, src, cw, ch := badgePlacement(mon, w, h, at, pad, margin, p)
			if dst.X < mon.Left || dst.Y < mon.Top || dst.X+cw > mon.Right || dst.Y+ch > mon.Bottom || cw <= 0 || ch <= 0 {
				t.Fatalf("%s at %.1f: %v %dx%d leaves the screen", at, p, dst, cw, ch)
			}
			if src.X < 0 || src.Y < 0 || src.X+cw > w || src.Y+ch > h {
				t.Fatalf("%s at %.1f: source %v %dx%d outside the frame", at, p, src, cw, ch)
			}
		}
		// The shape's top left on the screen, slid in.
		dst, src, _, _ := badgePlacement(mon, w, h, at, pad, margin, 1)
		x, y := dst.X-src.X+pad, dst.Y-src.Y+pad
		wantX := map[bool]int32{true: mon.Left + margin}[at.left()]
		switch {
		case at.right():
			wantX = mon.Right - margin - sw
		case !at.left():
			wantX = mon.Left + (mon.w()-sw)/2
		}
		wantY := mon.Bottom - margin - sh
		if at.top() {
			wantY = mon.Top + margin
		}
		if x != wantX || y != wantY {
			t.Errorf("%s slid in: shape at %d,%d, want %d,%d", at, x, y, wantX, wantY)
		}
		// Hidden, it touches its edge.
		dst, src, _, ch := badgePlacement(mon, w, h, at, pad, margin, 0)
		top, bottom := dst.Y-src.Y+pad, dst.Y-src.Y+pad+sh
		if at.top() && top != mon.Top || !at.top() && bottom != mon.Bottom {
			t.Errorf("%s hidden: shape from %d to %d (shown %d high), want it at the edge", at, top, bottom, ch)
		}
	}
}
