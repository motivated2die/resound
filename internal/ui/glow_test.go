package ui

import "testing"

func TestGlowPixels(t *testing.T) {
	const w, h = 120, 160
	g := glowSpec{strength: 0.2, falloff: 3}
	// Light centred on the right edge at half height, like edgeGlow.
	pix := glowPixels(w, h, w, h/2, w, h/2, rgb(0x50, 0xDE, 0xA8), g)
	alpha := func(x, y int) int { return int(pix[4*(y*w+x)+3]) }

	if c, mid, edge := alpha(w-1, h/2), alpha(w/2, h/2), alpha(0, h/2); !(c > mid && mid > edge) {
		t.Errorf("opacity along the middle: right %d, middle %d, left %d; want decreasing", c, mid, edge)
	}
	// The box's border is fully transparent, so the glow has no edge.
	for _, p := range [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}, {0, h / 2}} {
		if a := alpha(p[0], p[1]); a > 1 {
			t.Errorf("pixel %v opacity %d, want transparent", p, a)
		}
	}
	if a := alpha(w-1, h/2); a < 45 || a > 56 {
		t.Errorf("centre opacity %d, want about %d (20%%)", a, 51)
	}
	// Premultiplied: no channel exceeds the opacity.
	for i := 0; i < len(pix); i += 4 {
		if pix[i] > pix[i+3] || pix[i+1] > pix[i+3] || pix[i+2] > pix[i+3] {
			t.Fatalf("pixel %d is not premultiplied: %v", i/4, pix[i:i+4])
		}
	}
}

// A higher falloff makes a tighter light: dimmer halfway out.
func TestGlowFalloff(t *testing.T) {
	const n = 100
	at := func(falloff float64) uint8 {
		pix := glowPixels(n, 1, 0, 0.5, n, 1, rgb(0x50, 0xDE, 0xA8), glowSpec{strength: 1, falloff: falloff})
		return pix[4*(n/2)+3]
	}
	if soft, tight := at(3), at(5.5); tight >= soft {
		t.Errorf("halfway opacity: falloff 3 → %d, 5.5 → %d; want the tighter one dimmer", soft, tight)
	}
}
