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
