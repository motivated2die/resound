package ui

import "testing"

func TestOverlayGeometry(t *testing.T) {
	if h := overlayBarHeight(3); h != 3*44+2*6+2*6 {
		t.Errorf("bar height for 3: %d", h)
	}
	// As many buttons as fit, at least one.
	if v := overlayVisible(20, overlayBarHeight(7)); v != 7 {
		t.Errorf("visible in a 7-button bar: %d", v)
	}
	if v := overlayVisible(3, 2000); v != 3 {
		t.Errorf("visible of 3: %d", v)
	}
	if v := overlayVisible(5, 10); v != 1 {
		t.Errorf("visible in a tiny bar: %d", v)
	}
	// The highlight stays in the middle, except near the ends.
	for _, c := range []struct{ n, vis, sel, want int }{
		{20, 7, 0, 0}, {20, 7, 3, 0}, {20, 7, 10, 7}, {20, 7, 19, 13}, {5, 7, 4, 0},
	} {
		if got := overlayFirst(c.n, c.vis, c.sel); got != c.want {
			t.Errorf("overlayFirst(%d, %d, %d) = %d, want %d", c.n, c.vis, c.sel, got, c.want)
		}
	}
	if clampSelection(9, 4) != 3 || clampSelection(-2, 4) != 0 || clampSelection(0, 0) != -1 {
		t.Error("clampSelection")
	}
}

func TestInitials(t *testing.T) {
	for in, want := range map[string]string{
		"Bruh": "Br", "fa": "fa", "FU": "FU", "x": "x", "  -- ge whiz": "ge",
		"5 4 3 2 1 go": "54", "Ärger": "Är", "!!!": "!", "": "?",
	} {
		if got := Initials(in); got != want {
			t.Errorf("Initials(%q) = %q, want %q", in, got, want)
		}
	}
}

// The overlay never reaches past its screen while it slides in, on either
// side, also with a screen beside it (left screen of two here).
func TestOverlayPlacement(t *testing.T) {
	mon := rect{0, 0, 2560, 1440}
	const w, h, edge = 400, 600, 30
	for _, left := range []bool{false, true} {
		for _, p := range []float64{0, 0.25, 0.5, 1} {
			dst, src, cw, ch := overlayPlacement(mon, w, h, left, edge, p)
			if dst.X < mon.Left || dst.X+cw > mon.Right || dst.Y < mon.Top || dst.Y+ch > mon.Bottom {
				t.Errorf("left=%v p=%v: shown part %d,%d %dx%d outside %+v", left, p, dst.X, dst.Y, cw, ch, mon)
			}
			if src.X < 0 || src.X+cw > w {
				t.Errorf("left=%v p=%v: source %d+%d outside the frame", left, p, src.X, cw)
			}
		}
		// In place the whole frame shows, flush with the screen's edge.
		dst, src, cw, _ := overlayPlacement(mon, w, h, left, edge, 1)
		if cw != w || src.X != 0 || (left && dst.X != 0) || (!left && dst.X+cw != mon.Right) {
			t.Errorf("left=%v in place: dst %d src %d width %d", left, dst.X, src.X, cw)
		}
		// At the start the frame is moved out by edge and cut there.
		if _, src, cw, _ := overlayPlacement(mon, w, h, left, edge, 0); cw != w-edge || (left && src.X != edge) {
			t.Errorf("left=%v at the start: src %d width %d", left, src.X, cw)
		}
	}
}
