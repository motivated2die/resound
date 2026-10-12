package ui

import "testing"

func TestDashedRoundRectFollowsCorners(t *testing.T) {
	const w, h, r = 300, 100, 12.0
	mask := dashedRoundRect(w, h, r, 1, 6, 4)
	covered := func(x0, y0, x1, y1 int) bool {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if mask.AlphaAt(x, y).A > 0 {
					return true
				}
			}
		}
		return false
	}
	// The arc passes through each corner's diagonal region (about r(1-1/√2)
	// in from both edges); a square corner would leave it empty there.
	for _, c := range [][4]int{{2, 2, 6, 6}, {w - 6, 2, w - 2, 6}, {2, h - 6, 6, h - 2}, {w - 6, h - 6, w - 2, h - 2}} {
		if !covered(c[0], c[1], c[2], c[3]) {
			t.Errorf("no dash along the corner arc in %v", c)
		}
	}
	// The very corner pixels stay empty (rounded, not square).
	if covered(0, 0, 2, 2) || covered(w-2, h-2, w, h) {
		t.Error("outline reaches the square corner")
	}
	// Dashed, not solid: the top edge has gaps.
	gaps := 0
	for x := 20; x < w-20; x++ {
		if mask.AlphaAt(x, 0).A == 0 {
			gaps++
		}
	}
	if gaps < 50 {
		t.Errorf("top edge looks solid (%d empty pixels)", gaps)
	}
}
