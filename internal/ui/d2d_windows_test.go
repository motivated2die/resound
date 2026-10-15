package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// The renderer draws anti-aliased shapes with transparency around them,
// text, and emoji in color.
func TestD2DDraws(t *testing.T) {
	g, err := getD2D()
	if err != nil {
		t.Skipf("no Direct2D: %v", err)
	}
	s, err := newSurface(120, 60)
	if err != nil {
		t.Fatal(err)
	}
	defer s.free()
	p, err := g.begin(s)
	if err != nil {
		t.Fatal(err)
	}
	p.fillCircle(30, 30, 28, rgbaF(rgb(0x30, 0x30, 0x30), 1))
	p.text("🍌", rectF{2, 2, 58, 58}, fontSpec{family: "Segoe UI Emoji", size: 30, weight: 400, align: alignCenter}, colorF{1, 1, 1, 1})
	p.text("Bruh", rectF{62, 0, 120, 60}, fontSpec{family: "Segoe UI", size: 18, weight: 600}, colorF{1, 1, 1, 1})
	if err := p.end(); err != nil {
		t.Fatal(err)
	}
	px := func(x, y int) (b, g, r, a uint8) {
		i := (y*int(s.w) + x) * 4
		return s.bits[i], s.bits[i+1], s.bits[i+2], s.bits[i+3]
	}
	if _, _, _, a := px(0, 0); a != 0 {
		t.Errorf("corner outside the circle: alpha %d, want 0", a)
	}
	if _, _, _, a := px(30, 4); a != 255 {
		t.Errorf("inside the circle: alpha %d, want 255", a)
	}
	colored, text := 0, 0
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			b, g, r, a := px(x, y)
			if x < 60 && a == 255 && int(r)-int(b) > 60 && int(g)-int(b) > 40 {
				colored++ // the banana's yellow
			}
			if x >= 62 && a > 128 {
				text++
			}
		}
	}
	if colored < 20 {
		t.Errorf("emoji not in color: %d yellow pixels", colored)
	}
	if text < 20 {
		t.Errorf("text not drawn: %d pixels", text)
	}
	if w := g.measure("Bruh", fontSpec{family: "Segoe UI", size: 18, weight: 600}); w < 25 || w > 60 {
		t.Errorf("measured width %v", w)
	}
}

// overlayTestItems are favorites like the ones in the design mock-up.
var overlayTestItems = []OverlayItem{
	{Name: "FU"}, {Name: "Lip bite", Emoji: "🫦", Color: "red"}, {Name: "Banana", Emoji: "🍌", Color: "yellow"},
	{Name: "Bruh", Emoji: "😠"}, {Name: "Car horn", Emoji: "🚗", Color: "blue"}, {Name: "Cactus", Emoji: "🌵", Color: "green"},
	{Name: "fa", Color: "blue"}, {Name: "ge"},
}

// The overlay renders with transparency around it and its highlight inside
// the frame, on both sides; with RESOUND_RENDER_DIR set, frames are saved
// as PNGs over a dark background for a look.
func TestRenderOverlay(t *testing.T) {
	if _, err := getD2D(); err != nil {
		t.Skipf("no Direct2D: %v", err)
	}
	many := append(append(append([]OverlayItem{}, overlayTestItems...), overlayTestItems...), overlayTestItems...)
	scenes := map[string]overlayScene{
		"right":     {items: overlayTestItems, sel: 3, scale: 1.5, maxH: 800, nameMax: ovNameMax, shadow: ovShadowGame},
		"left":      {items: overlayTestItems, sel: 3, left: true, scale: 1.5, maxH: 800, nameMax: ovNameMax, shadow: ovShadowGame},
		"flash":     {items: overlayTestItems, sel: 3, scale: 1.5, maxH: 800, nameMax: ovNameMax, flash: flashOK, shadow: ovShadowGame},
		"long name": {items: []OverlayItem{{Name: "A very long sound name that goes on", Emoji: "📢", Color: "orange"}}, scale: 1.5, maxH: 800, nameMax: ovNameMax, shadow: ovShadowGame},
		"scrolling": {items: many, sel: 12, scale: 1.5, maxH: 500, nameMax: ovNameMax, shadow: ovShadowGame},
		"empty":     {scale: 1.5, empty: "Star sounds on your phone to see them here", shadow: ovShadowGame},
		"holding":   {items: overlayTestItems, sel: 3, scale: 1.5, maxH: 800, nameMax: ovNameMax, hold: 0.6, shadow: ovShadowGame},
	}
	for name, sc := range scenes {
		s, err := renderOverlay(sc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if a := s.bits[3]; a != 0 {
			t.Errorf("%s: top left corner alpha %d, want 0", name, a)
		}
		if dir := os.Getenv("RESOUND_RENDER_DIR"); dir != "" {
			img := image.NewNRGBA(image.Rect(0, 0, int(s.w), int(s.h)))
			for i := 0; i < int(s.w*s.h); i++ {
				b, g, r, a := s.bits[4*i], s.bits[4*i+1], s.bits[4*i+2], s.bits[4*i+3]
				bg := uint32(0x2B) // a dark scene: premultiplied over it
				img.Pix[4*i] = uint8(uint32(r) + bg*(255-uint32(a))/255)
				img.Pix[4*i+1] = uint8(uint32(g) + (bg+0x0A)*(255-uint32(a))/255)
				img.Pix[4*i+2] = uint8(uint32(b) + (bg-0x08)*(255-uint32(a))/255)
				img.Pix[4*i+3] = 255
			}
			f, err := os.Create(filepath.Join(dir, "overlay-"+name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			_ = png.Encode(f, img)
			f.Close()
		}
		s.free()
	}
}

// The overlay's preview row lays out beside its text in the panel.
func TestOverlayPreviewRow(t *testing.T) {
	if _, err := getD2D(); err != nil {
		t.Skipf("no Direct2D: %v", err)
	}
	rows := []Row{
		Header("In-game overlay", func() {}),
		OverlayPreview("Press F8 in a game, scroll to a favorite and press F8 again to play it.",
			"Esc closes the bar. Hold Esc to also stop all sounds.", false),
		Section("Key"),
		Text("F8", "", ToneGood),
		Buttons(Action{Label: "Change key"}, Action{Label: "Turn off"}),
		Section("Screen edge"),
		Segmented([]string{"Left", "Right"}, 1, false, func(int) {}),
		Section("Turned off"),
		Segmented([]string{"Left", "Right"}, 1, true, func(int) {}),
	}
	// A measuring DC, so paragraphs wrap as in the panel.
	img := renderRowsMeasured(t, rows, 1.5)
	savePNG(t, img, "overlay-page")
}

// renderRowsMeasured is renderRows measuring with a device context, so
// wrapped text gets its height as in the panel.
func renderRowsMeasured(t *testing.T, rows []Row, scale float64) image.Image {
	t.Helper()
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	f := newFonts(scale)
	defer f.destroy()
	m := &canvas{dc: dc, scale: scale, fonts: f}
	w := m.px(popupWidthL)
	heights, total := make([]int32, len(rows)), int32(0)
	for i, r := range rows { // as Popup.layout does
		heights[i] = m.px(r.height())
		if ms, ok := r.(measurer); ok {
			heights[i] = ms.measure(m, w)
		}
		total += heights[i]
	}
	h := total + 2*m.px(popupPadL)
	return renderCanvas(t, w, h, scale, func(c *canvas) {
		c.fill(rect{0, 0, w, h}, colBackground)
		paintRows(c, rows, heights, 0, c.px(popupPadL), nil)
	})
}

func TestTruncateName(t *testing.T) {
	for in, want := range map[string]string{
		"Bruh":                      "Bruh",
		"Exactly sixteen!":          "Exactly sixteen!",
		"A very long sound name":    "A very long soun…",
		"Ends at a space here too":  "Ends at a space…",
		"Ärger über Ölpreise heute": "Ärger über Ölpre…",
	} {
		if got := TruncateName(in, 16); got != want {
			t.Errorf("TruncateName(%q) = %q, want %q", in, got, want)
		}
	}
}
