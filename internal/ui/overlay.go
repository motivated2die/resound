package ui

import (
	"unicode"
	"unicode/utf8"
)

// The in-game overlay (overlay_windows.go): a vertical bar of round buttons,
// one per favorite, at the left or right edge of the primary screen. The
// highlighted one grows into a pill towards the middle of the screen, with
// the sound's name beside its icon. This file holds its portable geometry.

// OverlayItem is one favorite in the overlay.
type OverlayItem struct {
	Name  string
	Emoji string // shown instead of the name's first letters when set
	Color string // the phone's card color: "red", "blue", …; "" = default
}

// Logical (96-DPI) sizes.
const (
	ovBar      = 56    // bar width
	ovDot      = 44    // a button's diameter
	ovGap      = 6     // between buttons
	ovInset    = 6     // the bar's padding around its buttons
	ovPill     = 52    // the highlight's height
	ovNameMax  = 220   // widest name in the highlight; longer ones are trimmed
	ovNameGap  = 10    // between the name and the icon
	ovNameLead = 20    // the highlight's padding before the name
	ovPad      = 22    // room for the shadow around everything
	ovEdge     = ovPad // between the bar and the screen edge: the window stays on its screen
	// ovMaxShare is the share of the screen height the bar may take; more
	// favorites than fit scroll within it.
	ovMaxShare = 0.7
)

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
