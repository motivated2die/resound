package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The in-game overlay (overlay_windows.go): a vertical bar of round buttons,
// one per favorite, at the left or right edge of the primary screen. The
// highlighted one is a pill reaching towards the middle of the screen, with
// the sound's name beside its icon. This file holds its portable geometry.

// OverlayItem is one favorite in the overlay.
type OverlayItem struct {
	Name  string
	Emoji string // shown instead of the name's first letters when set
	Color string // the phone's card color: "red", "blue", …; "" = default
}

// Logical (96-DPI) sizes.
const (
	ovBar       = 56  // bar width
	ovDot       = 44  // a button's diameter
	ovGap       = 6   // between buttons
	ovInset     = 6   // the bar's padding around its buttons
	ovPill      = 52  // the highlight's height
	ovNameChars = 16  // most characters of a name shown; longer ones end in "…"
	ovNameMax   = 220 // widest name in the highlight, a safety net for wide letters
	ovNameGap   = 10  // between the name and the icon
	ovNameLead  = 20  // the highlight's padding before the name
	ovEdge      = 20  // between the bar and the screen edge; the window's padding on that side
	// ovMaxShare is the share of the screen height the bar may take; more
	// favorites than fit scroll within it.
	ovMaxShare = 0.7
)

// overlayShadow is a drop shadow, logical: blur radius, spread (the shape
// grown before blurring), offset downwards, the darkest alpha before the
// cap, and the cap (0..1), so a wide shadow never blacks out the game.
type overlayShadow struct {
	blur, spread, offY int
	opacity, maxAlpha  float64
}

var (
	// In a game the bar casts a wide, dark shadow that sets it apart from
	// any scene; in the panel's picture a modest one.
	ovShadowGame  = overlayShadow{blur: 64, spread: 14, offY: 6, opacity: 0.72, maxAlpha: 0.5}
	ovShadowPanel = overlayShadow{blur: 14, offY: 4, opacity: 0.5, maxAlpha: 0.5}
)

// pad is the room the shadow needs around its shape.
func (s overlayShadow) pad() int { return s.blur + s.spread + s.offY }

// ovReach is how far the highlight reaches past the button's inner edge.
const ovReach = ovNameGap + ovNameMax + ovNameLead

// overlayBarHeight is the bar's logical height with n buttons.
func overlayBarHeight(n int) int {
	n = max(n, 1)
	return n*ovDot + (n-1)*ovGap + 2*ovInset
}

// overlayVisible is how many of n buttons fit into a bar at most maxH
// logical pixels tall (at least one).
func overlayVisible(n, maxH int) int {
	fit := max(1, (maxH-2*ovInset+ovGap)/(ovDot+ovGap))
	return max(1, min(n, fit))
}

// overlayFirst is the first of visible buttons shown when sel is
// highlighted: sel stays in the middle while the list allows.
func overlayFirst(n, visible, sel int) int {
	return max(0, min(sel-visible/2, n-visible))
}

// clampSelection keeps a highlight within n items (-1 when there are none).
func clampSelection(sel, n int) int {
	if n == 0 {
		return -1
	}
	return max(0, min(sel, n-1))
}

// TruncateName shortens a name to at most n characters, ending in "…"
// when anything was cut.
func TruncateName(name string, n int) string {
	if utf8.RuneCountInString(name) <= n {
		return name
	}
	r := []rune(name)[:n]
	return strings.TrimRight(string(r), " ") + "…"
}

// Initials returns what a button without an emoji shows: the first two
// letters or digits of the name, as written ("Bruh" → "Br").
func Initials(name string) string {
	var out []rune
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, r)
			if len(out) == 2 {
				break
			}
		}
	}
	if len(out) == 0 {
		if r, _ := utf8.DecodeRuneInString(name); r != utf8.RuneError {
			return string(r)
		}
		return "?"
	}
	return string(out)
}

// overlayColors are the phone's card colors (webapp styles.css,
// --sound-color-*-bg), a little lighter so they read on any game. "" is
// the phone's default card.
var overlayColors = map[string]uint32{
	"":       rgb(0x3E, 0x40, 0x49),
	"red":    rgb(0x5C, 0x34, 0x34),
	"orange": rgb(0x6C, 0x4A, 0x27),
	"yellow": rgb(0x6A, 0x5F, 0x33),
	"green":  rgb(0x3F, 0x5A, 0x33),
	"blue":   rgb(0x33, 0x47, 0x5A),
	"purple": rgb(0x47, 0x33, 0x5A),
	"pink":   rgb(0x5A, 0x33, 0x47),
	"grey":   rgb(0x54, 0x56, 0x5F),
	"lime":   rgb(0x5A, 0x66, 0x33),
	"teal":   rgb(0x2A, 0x5C, 0x4C),
	"sky":    rgb(0x38, 0x64, 0x7B),
}

func overlayColor(name string) uint32 {
	if c, ok := overlayColors[name]; ok {
		return c
	}
	return overlayColors[""]
}

// easeOut is a cubic ease-out over t in 0..1.
func easeOut(t float64) float64 {
	t = max(0, min(1, t))
	u := 1 - t
	return 1 - u*u*u
}

// overlayPlacement places a w×h frame at the left or right edge of mon,
// slid in by p (0..1): at 0 its bar touches the screen's edge, at 1 it is
// edge pixels away (the frame's padding on that side). What would fall off
// mon is cut, so the frame never shows on a screen beside it. It returns
// where the shown part goes on the screen, where it starts in the frame,
// and its size.
func overlayPlacement(mon rect, w, h int32, left bool, edge int32, p float64) (dst, src point, cw, ch int32) {
	travel := int32(float64(edge) * (1 - max(0, min(1, p))))
	y := mon.Top + (mon.Bottom-mon.Top-h)/2
	cw, ch = w, h
	if left {
		x := mon.Left - travel
		src.X = mon.Left - x
		dst = point{mon.Left, y}
		cw -= src.X
	} else {
		x := mon.Right - w + travel
		dst = point{x, y}
		cw = min(w, mon.Right-x)
	}
	if y < mon.Top { // taller than the screen: shouldn't happen, cut the top
		src.Y, dst.Y, ch = mon.Top-y, mon.Top, h-(mon.Top-y)
	}
	ch = min(ch, mon.Bottom-dst.Y)
	return dst, src, cw, ch
}
