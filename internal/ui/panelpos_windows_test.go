package ui

import "testing"

func TestDockedPos(t *testing.T) {
	work := rect{0, 0, 1920, 1032} // taskbar at the bottom
	// Above the taskbar, centred on the icon, kept off the screen's edge.
	if got := dockedPos(point{1700, 1050}, work, 340, 560, 12, 24); got != (point{1530, 460}) {
		t.Errorf("bottom taskbar: %v", got)
	}
	if got := dockedPos(point{1910, 1050}, work, 340, 560, 12, 24); got.X != 1920-12-340 {
		t.Errorf("icon at the right edge: %v", got)
	}
	// Taskbar at the top: below it, with room for the tab.
	if got := dockedPos(point{1700, 10}, rect{0, 48, 1920, 1080}, 340, 560, 12, 24); got != (point{1530, 48 + 12 + 24}) {
		t.Errorf("top taskbar: %v", got)
	}
	// Taskbar on the left, icon at its bottom: against the work area's left edge.
	if got := dockedPos(point{24, 1050}, rect{48, 0, 1920, 1080}, 340, 560, 12, 24); got != (point{60, 1080 - 12 - 560}) {
		t.Errorf("left taskbar: %v", got)
	}
}

func TestClampPanel(t *testing.T) {
	work := rect{0, 0, 1920, 1032}
	for _, c := range []struct {
		pos, want point
	}{
		{point{500, 300}, point{500, 300}},             // inside: unchanged
		{point{-80, 300}, point{0, 300}},               // past the left edge
		{point{1800, 300}, point{1920 - 340, 300}},     // past the right edge
		{point{500, 10}, point{500, 24}},               // the tab above the top
		{point{500, 900}, point{500, 1032 - 560}},      // below the taskbar
		{point{5000, -400}, point{1920 - 340, 24}},     // far away
		{point{-3000, 5000}, point{0, 1032 - 560}},     // on no screen at all
		{point{1920 - 340, 24}, point{1920 - 340, 24}}, // flush in the corner
	} {
		if got := clampPanel(c.pos, work, 340, 560, 24); got != c.want {
			t.Errorf("clampPanel(%v) = %v, want %v", c.pos, got, c.want)
		}
	}
	// A second screen left of the first, at negative coordinates.
	left := rect{-2560, -200, 0, 1240}
	if got := clampPanel(point{-100, 1000}, left, 510, 840, 36); got != (point{-510, 1240 - 840}) {
		t.Errorf("left screen: %v", got)
	}
	// Taller than the work area: the tab stays visible.
	if got := clampPanel(point{0, 0}, rect{0, 0, 800, 500}, 340, 560, 24); got.Y != 24 {
		t.Errorf("too tall: %v", got)
	}
}
