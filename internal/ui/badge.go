package ui

// The launchpad's badge (badge_windows.go): while the launchpad key is held,
// a round yellow button shows at a corner or the middle of the top or bottom
// edge of the primary screen, in a dark capsule with the open tab's name
// beside it; a sound played from it shows there for a moment. This file
// holds its portable geometry.

// BadgeAt is where the badge shows.
type BadgeAt int

const (
	BadgeTopLeft BadgeAt = iota
	BadgeTopCenter
	BadgeTopRight
	BadgeBottomLeft
	BadgeBottomCenter
	BadgeBottomRight
)

// BadgeSpots are the places in the panel's order: the top row, then the
// bottom one, left to right.
var BadgeSpots = []BadgeAt{BadgeTopLeft, BadgeTopCenter, BadgeTopRight, BadgeBottomLeft, BadgeBottomCenter, BadgeBottomRight}

var badgeAtNames = [...]string{"top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"}

// String is the place's name in config.json.
func (b BadgeAt) String() string { return badgeAtNames[b] }

// ParseBadgeAt reads a place's name; anything else is the default, the top
// center.
func ParseBadgeAt(s string) BadgeAt {
	for i, n := range badgeAtNames {
		if n == s {
			return BadgeAt(i)
		}
	}
	return BadgeTopCenter
}

func (b BadgeAt) top() bool   { return b <= BadgeTopRight }
func (b BadgeAt) left() bool  { return b == BadgeTopLeft || b == BadgeBottomLeft }
func (b BadgeAt) right() bool { return b == BadgeTopRight || b == BadgeBottomRight }

// Logical (96-DPI) sizes.
const (
	bdHeight   = 56  // the capsule's height; without text, its width too
	bdDot      = 44  // the yellow button's diameter
	bdTextGap  = 12  // between the button and the text
	bdTextEnd  = 22  // after the text
	bdTextMax  = 280 // widest text, a safety net for wide letters
	bdChars    = 28  // most characters of a name shown; longer ones end in "…"
	bdMargin   = 20  // between the capsule and the screen's edges, once slid in
	bdEmojiGap = 8   // between a sound's emoji and its name
)

// badgePlacement places a w×h frame, a shape with pad pixels of room for
// its shadow all round, at its place on mon: margin pixels from the edges
// once slid in (p = 1), touching the top or bottom edge at p = 0. What
// would fall off mon is cut, so nothing of it shows on a screen beside it.
func badgePlacement(mon rect, w, h int32, at BadgeAt, pad, margin int32, p float64) (dst, src point, cw, ch int32) {
	sw, sh := w-2*pad, h-2*pad
	var x, y int32
	switch {
	case at.left():
		x = mon.Left + margin
	case at.right():
		x = mon.Right - margin - sw
	default:
		x = mon.Left + (mon.w()-sw)/2
	}
	travel := int32(float64(margin) * (1 - max(0, min(1, p))))
	if at.top() {
		y = mon.Top + margin - travel
	} else {
		y = mon.Bottom - margin - sh + travel
	}
	return clipFrame(mon, x-pad, y-pad, w, h)
}

// clipFrame cuts a w×h frame at x, y to mon: where its shown part goes, where
// that starts in the frame, and its size.
func clipFrame(mon rect, x, y, w, h int32) (dst, src point, cw, ch int32) {
	dst = point{max(x, mon.Left), max(y, mon.Top)}
	src = point{dst.X - x, dst.Y - y}
	cw = min(x+w, mon.Right) - dst.X
	ch = min(y+h, mon.Bottom) - dst.Y
	return dst, src, cw, ch
}
