package ui

// --- ScreenSpots -------------------------------------------------------------

type spotsRow struct {
	selected BadgeAt
	disabled bool
	onSelect func(BadgeAt)
}

// ScreenSpots picks where the launchpad's badge shows: a small screen with
// a spot at each place it can take (BadgeSpots), the chosen one in the
// launchpad's yellow. Disabled greys it out (the badge is off) and ignores
// clicks.
func ScreenSpots(selected BadgeAt, disabled bool, onSelect func(BadgeAt)) Row {
	return spotsRow{selected, disabled, onSelect}
}

func (spotsRow) height() int     { return 108 }
func (spotsRow) keepsOpen() bool { return true }

// screen is the small screen, centred in the row.
func (s spotsRow) screen(c *canvas, r rect) rect {
	in := inset(c, r)
	w, h := c.px(172), c.px(96)
	left := in.Left + (in.w()-w)/2
	top := r.Top + (r.h()-h)/2
	return rect{left, top, left + w, top + h}
}

// spots are the places on the screen, in BadgeSpots' order.
func (s spotsRow) spots(c *canvas, r rect) []rect {
	sc := s.screen(c, r)
	w, h, m := c.px(28), c.px(12), c.px(10)
	out := make([]rect, len(BadgeSpots))
	for i, at := range BadgeSpots {
		x := sc.Left + (sc.w()-w)/2
		switch {
		case at.left():
			x = sc.Left + m
		case at.right():
			x = sc.Right - m - w
		}
		y := sc.Bottom - m - h
		if at.top() {
			y = sc.Top + m
		}
		out[i] = rect{x, y, x + w, y + h}
	}
	return out
}

// segments are the spots with room around them, so they are easy to hit.
func (s spotsRow) segments(c *canvas, r rect) []rect {
	if s.disabled {
		return nil
	}
	sc := s.screen(c, r)
	out := s.spots(c, r)
	for i, b := range out {
		out[i] = rect{max(sc.Left, b.Left-c.px(10)), max(sc.Top, b.Top-c.px(8)), min(sc.Right, b.Right+c.px(10)), min(sc.Bottom, b.Bottom+c.px(8))}
	}
	return out
}

func (s spotsRow) click(seg int) {
	if seg >= 0 && seg < len(BadgeSpots) && BadgeSpots[seg] != s.selected && s.onSelect != nil {
		s.onSelect(BadgeSpots[seg])
	}
}

func (s spotsRow) paint(c *canvas, r rect, st rowState) {
	border, chosen := colControl, LaunchpadYellow
	if s.disabled {
		border, chosen = colDivider, colCaption
	}
	c.frame(s.screen(c, r), c.px(10), mix(colBackground, colList, 0.5), border)
	for i, b := range s.spots(c, r) {
		color := colControl
		switch {
		case BadgeSpots[i] == s.selected:
			color = chosen
		case s.disabled:
			color = colDivider
		case st.press == i:
			color = colText
		case st.hover == i:
			color = colMuted
		}
		c.roundRect(b, b.h(), color)
	}
}
