package ui

import "testing"

// The launchpad's page: the screen with its six spots under the badge's
// switches. RESOUND_RENDER_DIR saves a PNG of it.
func TestScreenSpotsRender(t *testing.T) {
	picked := BadgeTopCenter
	spots := ScreenSpots(BadgeBottomRight, false, func(at BadgeAt) { picked = at })
	rows := []Row{
		HeaderButton("Launchpad", func() {}, IconHelp, "Help", "", func() {}),
		Paragraph("Hold F7 in a game: your phone shows a key on each button of its tab, 1 to 9, 0, then A to Z. Press one to play that sound.", ToneMuted),
		Paragraph("While you hold it, Space stops all sounds, and the arrow keys or the mouse wheel switch tabs. Your game gets none of these keys meanwhile.", ToneMuted),
		Section("Key"),
		Keybind(KeybindSpec{ID: "k", Icon: IconLaunchpad, Title: "Launchpad", Info: "i", Keys: []string{"F7"}, OnAdd: func() {}, OnRemove: func(int) {}}),
		ParagraphLink("A game can have a launchpad key of its own, or no launchpad at all.", "Set it in App keybinds", "", func() {}),
		Section("On-screen badge"),
		ToggleInfo("Show a badge while you hold the key", "i", true, func() {}),
		ToggleInfo("Show the tab's name", "i", true, func() {}),
		ToggleInfo("Show the sounds you play", "i", false, func() {}),
		spots,
		ScreenSpots(BadgeTopLeft, true, nil),
	}
	savePNG(t, renderRowsMeasured(t, rows, 1.5), "launchpad-page")

	r := spots.(spotsRow)
	r.click(0)
	if picked != BadgeTopLeft {
		t.Fatalf("clicking the first spot picked %s", picked)
	}
	picked = BadgeTopCenter
	r.click(5) // the chosen one
	if picked != BadgeTopCenter {
		t.Fatal("clicking the chosen spot picked it again")
	}
}
