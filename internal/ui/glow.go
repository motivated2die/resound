package ui

import "math"

// The panel's background glow: soft accent-coloured light in the top left
// corner, as if a blurred dot sat just outside the panel. It belongs to the
// window, not the content: it stays in place when the page scrolls or the
// panel grows.

// glowSpec describes one glow in logical (96-DPI) pixels.
type glowSpec struct {
	radiusXL, radiusYL int     // how far the light reaches across and up/down
	strength           float64 // opacity of the accent colour at the centre (0..1)
	falloff            float64 // how quickly it fades; higher is a tighter light
}

// cornerGlow is the light in the top left corner.
var cornerGlow = glowSpec{radiusXL: 150, radiusYL: 140, strength: 0.18, falloff: 5.5}

// glowPixels renders a w×h box of accent-coloured light centred at (cx, cy)
// pixels (which may be on the box's edge or corner) with radii rx, ry, as
// premultiplied RGBA. The opacity follows a Gaussian falloff that reaches
// exactly zero at the ellipse's edge, so the box has no visible border.
// Overlapping glows add up. Interleaved-gradient-noise dithering of the
// opacity hides the banding a faint gradient on a dark background would
// show.
func glowPixels(w, h int, cx, cy, rx, ry float64, accent uint32, g glowSpec) []uint8 {
	pix := make([]uint8, 4*w*h)
	acC := [3]float64{float64(accent & 0xFF), float64(accent >> 8 & 0xFF), float64(accent >> 16 & 0xFF)}
	floor := math.Exp(-g.falloff)
	for y := range h {
		for x := range w {
			dx, dy := (float64(x)+0.5-cx)/rx, (float64(y)+0.5-cy)/ry
			t := dx*dx + dy*dy
			if t >= 1 {
				continue // transparent
			}
			a := g.strength * (math.Exp(-g.falloff*t) - floor) / (1 - floor)
			_, noise := math.Modf(52.9829189 * math.Mod(0.06711056*float64(x)+0.00583715*float64(y), 1))
			alpha := max(0, min(255, math.Round(a*255+noise-0.5)))
			i := 4 * (y*w + x)
			for c := range 3 {
				pix[i+c] = uint8(math.Round(acC[c] * alpha / 255))
			}
			pix[i+3] = uint8(alpha)
		}
	}
	return pix
}
