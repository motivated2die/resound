package ui

import "testing"

func TestShadowMask(t *testing.T) {
	const w, h, blur, off, corner = 200, 300, 18, 6, 8
	pad := blur + off
	mask, cw, ch := shadowMask(w, h, pad, blur, off, corner, 0.5)
	if cw != w+2*pad || ch != h+2*pad || len(mask) != cw*ch {
		t.Fatalf("size %d×%d (%d)", cw, ch, len(mask))
	}
	at := func(x, y int) int { return int(mask[y*cw+x]) }
	if c := at(cw/2, ch/2); c < 125 || c > 129 {
		t.Errorf("centre = %d, want 50%% of 255", c)
	}
	if at(0, 0) != 0 || at(cw-1, ch-1) > 1 {
		t.Errorf("canvas corners = %d, %d; want transparent", at(0, 0), at(cw-1, ch-1))
	}
	// Soft edge: at the panel's left edge the shadow is part-way.
	if e := at(pad, ch/2); e < 30 || e > 100 {
		t.Errorf("edge = %d, want a soft falloff", e)
	}
	// Symmetric left/right, and lowered: darker below the panel than above.
	if at(pad-4, ch/2) != at(cw-pad+3, ch/2) {
		t.Errorf("not symmetric: %d vs %d", at(pad-4, ch/2), at(cw-pad+3, ch/2))
	}
	if at(cw/2, pad-4) >= at(cw/2, ch-pad+3) {
		t.Errorf("above %d should be lighter than below %d", at(cw/2, pad-4), at(cw/2, ch-pad+3))
	}
}
