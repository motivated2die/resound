package ui

import "testing"

// An Offer grows with its text, and its only clickable part is its button,
// inside the frame. RESOUND_RENDER_DIR saves a PNG of the setup's welcome
// page with the import from Soundux.
func TestOfferRender(t *testing.T) {
	nop := func() {}
	short := Offer(IconImport, "Coming from Soundux?", "Carry it over.", "Import from Soundux…", nop).(offerRow)
	long := Offer(IconImport, "Coming from Soundux?", "Soundux is set up on this PC with 4 sound folders, keys and settings. Carry them over, then check them in the steps.", "Import from Soundux…", nop).(offerRow)
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	for _, scale := range []float64{1, 1.5} {
		f := newFonts(scale)
		c := &canvas{dc: dc, scale: scale, fonts: f}
		w := c.px(popupWidthL)
		if hs, hl := short.measure(c, w), long.measure(c, w); hl <= hs {
			t.Errorf("scale %v: a longer text didn't grow the card: %d <= %d", scale, hl, hs)
		}
		r := rect{0, 0, w, long.measure(c, w)}
		segs, frame := long.segments(c, r), long.frameBox(c, r)
		if len(segs) != 1 || segs[0].Left <= frame.Left || segs[0].Right >= frame.Right || segs[0].Bottom >= frame.Bottom || segs[0].h() != c.px(offerButtonL) {
			t.Errorf("scale %v: button %v outside the frame %v", scale, segs, frame)
		}
		f.destroy()
	}
	// As the welcome page lays them out (app.wizardWelcome).
	rows := []Row{
		Paragraph("Four short steps: add your sounds, set up your audio and voice chat, set your push-to-talk key and pair your phone.", ToneMuted),
		Spacer(6),
		Offer(IconImport, "Coming from Soundux?", "Found 4 sound folders, keys and settings to carry over. You check them in the steps.", "Import from Soundux…", nop),
		Spacer(6),
		Offer(IconImport, "Coming from Soundpad?", "Found 3 sound lists and settings. Carry your sounds over by category or by folder.", "Import from Soundpad…", nop),
		Spacer(7),
		Button("Start setup", ButtonPrimary, false, nop),
		Button("Skip, I'll set it up myself", ButtonNormal, false, nop),
	}
	savePNG(t, renderRowsMeasured(t, rows, 1.5), "offer")
}
