package ui

import "testing"

// Choices grow with the longer text, the two cards sit side by side
// without touching, and a click chooses the card under it.
// RESOUND_RENDER_DIR saves a PNG of the Soundpad import's page by category.
func TestChoicesRender(t *testing.T) {
	chosen := -1
	folder := Choice{Icon: IconFolder, Title: "By folder", Text: "Adds the folders your sounds are in, as they are. Your categories don't come over."}
	category := Choice{Icon: IconCategory, Title: "By category", Text: "Copies your sounds into a folder per category, in Soundpad's order."}
	row := Choices(folder, category, 1, func(i int) { chosen = i }).(choicesRow)
	short := Choices(Choice{Title: "A", Text: "a"}, Choice{Title: "B", Text: "b"}, 0, nil).(choicesRow)
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	for _, scale := range []float64{1, 1.5} {
		f := newFonts(scale)
		c := &canvas{dc: dc, scale: scale, fonts: f}
		w := c.px(popupWidthL)
		if hl, hs := row.measure(c, w), short.measure(c, w); hl <= hs {
			t.Errorf("scale %v: a longer text didn't grow the cards: %d <= %d", scale, hl, hs)
		}
		r := rect{0, 0, w, row.measure(c, w)}
		segs := row.segments(c, r)
		if len(segs) != 2 || segs[0].Right >= segs[1].Left || segs[0].w() != segs[1].w() || segs[0].Top < r.Top || segs[0].Bottom > r.Bottom {
			t.Errorf("scale %v: cards %v in %v", scale, segs, r)
		}
		f.destroy()
	}
	row.click(0)
	if chosen != 0 {
		t.Errorf("clicked the first card: %d", chosen)
	}

	nop := func() {}
	lists := []Option{{Label: "moti", Detail: "current"}, {Label: "moti"}, {Label: "backup-soundlist-2026-10-27"}}
	head := []Row{
		Header("Import from Soundpad", nop),
		Paragraph("Choose the sound list to carry over and how its sounds come into Resound, whose tabs are folders.", ToneMuted),
		Section("Sound list"),
		DropdownTool("list", IconFolderOpen, "Choose another list…", nop, lists, 0, func(int) {}),
		Paragraph("What Soundpad shows now · 10 sounds · 2 categories · 27 Oct, 17:12", ToneMuted),
		Section("Your sounds"),
	}
	byFolder := append(append([]Row{}, head...),
		Choices(category, folder, 1, nil),
		Section("Folders"),
		ToggleDetail("sounds", "4 of 5 sounds", `C:\Program Files\Soundpad\sounds`, true, nop),
		ToggleDetail("1 music", "4 of 50 sounds", `C:\MEGA\Computer\sound effects\1 music`, true, nop),
		ToggleDetail("A folder with a long name that doesn't fit", "120 of 5000+ sounds", "", false, nop),
		Separator(),
		Button("Import and continue", ButtonPrimary, false, nop),
	)
	savePNG(t, renderRowsMeasured(t, byFolder, 1.5), "choices-folder")
	rows := append(append([]Row{}, head...),
		Choices(category, folder, 0, nil),
		Section("Categories"),
		Text("Meine Sounds", "5 sounds", ToneMuted),
		Text("3", "4 sounds", ToneMuted),
		Section("Copy to"),
		Text("Soundpad sounds", "", ToneNormal),
		Paragraph(`C:\Users\me\Music\Soundpad sounds`, ToneMuted),
		Buttons(Action{Label: "Choose another folder…", Icon: IconFolderOpen, OnClick: nop}),
		Paragraph("Copies 9 sound files (22 MB) into a folder per category; a sound in several categories is copied into each. The originals stay where they are. 210.4 GB is free on C:.", ToneWarning),
		Separator(),
		Button("Copy, import and continue", ButtonPrimary, false, nop),
	)
	savePNG(t, renderRowsMeasured(t, rows, 1.5), "choices")
}
