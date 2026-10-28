package ui

// LaunchpadPreview is a small, fixed picture of the launchpad for the panel,
// beside paragraphs as in OverlayPreview: three of the phone's buttons in a
// dark screen, emoji only like the overlay's picture, each with its key in
// the launchpad's yellow in the corner, the middle one playing, as if its
// key had just been pressed. About as wide as the overlay's picture.
func LaunchpadPreview(underline string, paragraphs ...string) Row {
	return previewRow{paragraphs, underline, func(c *canvas) (*surface, int32) {
		return launchpadPreviewPicture(c), int32(float64(ovShadowPanel.pad()) * c.scale) // its shadow's room
	}}
}

// The picture's sizes, logical pixels at the panel's scale.
const (
	lpvPad     = 6  // the screen around the buttons
	lpvButton  = 36 // a button's side
	lpvGap     = 5  // between buttons
	lpvRadius  = 7  // a button's corners
	lpvScreenR = 11 // the screen's corners
	lpvKey     = 13 // a key's label
	lpvKeyIn   = 3  // from the button's corner
)

// lpvItems are the example buttons; the second one plays.
var lpvItems = []OverlayItem{
	{Name: "Airhorn", Emoji: "📯", Color: "red"},
	{Name: "Bruh", Emoji: "😑", Color: "purple"},
	{Name: "Applause", Emoji: "👏", Color: "green"},
}

var (
	lpvScreenColor  = rgbaF(rgb(0x1C, 0x1D, 0x22), 1)    // the phone's background
	lpvScreenBorder = rgbaF(LaunchpadYellow, 0.55)       // the screen glows while the key is held
	lpvKeyText      = rgbaF(rgb(0x2A, 0x22, 0x08), 1)    // as on the phone (--on-launchpad)
	lpvPlayEdge     = rgbaF(rgb(0xCE, 0x93, 0xD8), 1)    // the playing button's outline (the phone's purple)
	lpvPlayFill     = rgbaF(rgb(0xFF, 0xFF, 0xFF), 0.10) // its progress
)

var lpvCache struct {
	scale float64
	surf  *surface
}

func launchpadPreviewPicture(c *canvas) *surface {
	if lpvCache.surf != nil && lpvCache.scale == c.scale {
		return lpvCache.surf
	}
	surf, err := renderLaunchpadPreview(c.scale)
	if err != nil {
		return nil
	}
	lpvCache.surf.free()
	lpvCache.scale, lpvCache.surf = c.scale, surf
	return surf
}

func renderLaunchpadPreview(scale float64) (*surface, error) {
	g, err := getD2D()
	if err != nil {
		return nil, err
	}
	s := float32(scale)
	px := func(v int) float32 { return float32(v) * s }
	n := len(lpvItems)
	screenW := px(2*lpvPad + n*lpvButton + (n-1)*lpvGap)
	screenH := px(2*lpvPad + lpvButton)
	pad := ovShadowPanel.pad()
	surf, err := newSurface(int32(screenW+px(2*pad)+0.5), int32(screenH+px(2*pad)+0.5))
	if err != nil {
		return nil, err
	}
	screen := rectF{px(pad), px(pad), px(pad) + screenW, px(pad) + screenH}
	emojiFont := fontSpec{family: "Segoe UI Emoji", size: 18 * s, weight: 400, align: alignCenter}
	keyFont := fontSpec{family: "Segoe UI", size: 9.5 * s, weight: 800, align: alignCenter}

	p, err := g.begin(surf)
	if err != nil {
		surf.free()
		return nil, err
	}
	// The screen, its border a ring around it.
	border := px(1)
	p.fillRound(screen, px(lpvScreenR), lpvScreenBorder)
	p.fillRound(rectF{screen.L + border, screen.T + border, screen.R - border, screen.B - border}, px(lpvScreenR)-border, lpvScreenColor)
	for i, it := range lpvItems {
		x := screen.L + px(lpvPad+i*(lpvButton+lpvGap))
		b := rectF{x, screen.T + px(lpvPad), x + px(lpvButton), screen.T + px(lpvPad+lpvButton)}
		inner := b
		if i == 1 { // playing: an outline, and the progress over the left part
			p.fillRound(b, px(lpvRadius), lpvPlayEdge)
			edge := px(3) / 2
			inner = rectF{b.L + edge, b.T + edge, b.R - edge, b.B - edge}
		}
		p.fillRound(inner, px(lpvRadius)-1, rgbaF(overlayColor(it.Color), 1))
		if i == 1 {
			p.fillRound(rectF{inner.L, inner.T, inner.L + (inner.R-inner.L)*0.6, inner.B}, px(lpvRadius)-1, lpvPlayFill)
		}
		// A little down and left, clear of the key.
		p.text(it.Emoji, rectF{b.L, b.T + px(4), b.R - px(4), b.B}, emojiFont, rgbaF(rgb(0xFF, 0xFF, 0xFF), 1))
		key := rectF{b.R - px(lpvKeyIn+lpvKey), b.T + px(lpvKeyIn), b.R - px(lpvKeyIn), b.T + px(lpvKeyIn+lpvKey)}
		p.fillRound(key, px(4), rgbaF(LaunchpadYellow, 1))
		p.text(string(rune('1'+i)), key, keyFont, lpvKeyText)
	}
	if err := p.end(); err != nil {
		surf.free()
		return nil, err
	}
	shadow := newShadowLayer(surf)
	shadow.add(screen, px(lpvScreenR), s, ovShadowPanel)
	shadow.under(surf)
	return surf, nil
}
