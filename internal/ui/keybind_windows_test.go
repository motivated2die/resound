package ui

import "testing"

func testKeybinds() []Row {
	presets := []Option{{Label: "None", Detail: "off in this app"}, {Label: "Pause", Detail: "rarely used"}, {Label: "Mouse 4", Detail: "mouse"}}
	noop, remove := func() {}, func(int) {}
	return []Row{
		Section("Global"),
		Keybind(KeybindSpec{ID: "ptt", Icon: IconMic, Title: "Push-to-talk", Info: "Resound holds it while sounds play.",
			Keys: []string{"Pause", "Ctrl + Mouse 5"}, OnAdd: noop, AddTip: "Add a key", OnRemove: remove}),
		Keybind(KeybindSpec{ID: "pause", Icon: IconPause, Title: "Pause", Info: "Hold to pause.",
			OnAdd: noop}),
		Keybind(KeybindSpec{ID: "stop", Icon: IconStop, Title: "Stop", Info: "Stops all sounds.", Recording: true, Pressed: "Left Ctrl",
			Hint: "Mouse buttons 3–5 too, over this panel. Esc cancels.", Presets: presets, OnPreset: remove, OnCancel: noop}),
		Section("Valorant"),
		Keybind(KeybindSpec{ID: "app-ptt", Icon: IconMic, Title: "Push-to-talk", Info: "i", Scope: "Global",
			Keys: []string{"Pause"}, Inherited: true, OnAdd: noop, OnOff: noop}),
		Keybind(KeybindSpec{ID: "app-pause", Icon: IconPause, Title: "Pause", Info: "i", Keys: []string{"Mouse 4", "V", "Left Alt + Numpad 0", "Scroll Lock"},
			OnAdd: noop, OnRemove: remove, OnReset: noop, ResetTip: "Use the global keys"}),
		Keybind(KeybindSpec{ID: "app-stop", Icon: IconStop, Title: "Stop", Info: "i", Off: true, OffLabel: "None: off in this app",
			OnAdd: noop, OnReset: noop, Note: "X is already the pause key in Valorant.", NoteTone: ToneWarning}),
		ToggleInfo("Hold push-to-talk while sounds play", "Talk-through on the phone always holds it.", true, noop),
	}
}

// TestKeybindRender draws the keybind cards in their states (keys, none,
// recording, an app's inherited, own and off keys); see savePNG.
func TestKeybindRender(t *testing.T) {
	for _, scale := range []float64{1, 1.5} {
		img := renderRowsMeasured(t, testKeybinds(), scale)
		name := "keybinds"
		if scale != 1 {
			name += "@1.5x"
		}
		savePNG(t, img, name)
	}
}

// Chips wrap inside the card and never leave it; the card grows to hold
// them, and each removable chip has its own button.
func TestKeybindLayout(t *testing.T) {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	f := newFonts(1)
	defer f.destroy()
	c := &canvas{dc: dc, scale: 1, fonts: f}
	k := testKeybinds()[6].(keybindRow)
	h := k.measure(c, popupWidthL)
	l := k.layout(c, rect{0, 0, popupWidthL, h})
	rows := map[int32]bool{}
	for i, ch := range l.chips {
		if ch.box.Left < l.box.Left || ch.box.Right > l.box.Right || ch.box.Bottom > l.box.Bottom {
			t.Errorf("chip %d %v outside the card %v", i, ch.box, l.box)
		}
		if ch.del == (rect{}) || ch.del.Left < ch.box.Left || ch.del.Right > ch.box.Right {
			t.Errorf("chip %d: remove button %v", i, ch.del)
		}
		rows[ch.box.Top] = true
	}
	if len(rows) < 2 {
		t.Errorf("four keys should wrap onto two lines: %v", l.chips)
	}
	segs := k.segments(c, rect{0, 0, popupWidthL, h})
	if len(segs) != kbSegChips+4 || segs[kbSegReset] == (rect{}) || segs[kbSegAdd] == (rect{}) {
		t.Errorf("segments %v", segs)
	}
	var removed []int
	k.s.OnRemove = func(i int) { removed = append(removed, i) }
	k.click(kbSegChips + 2)
	if len(removed) != 1 || removed[0] != 2 {
		t.Errorf("clicking chip 2's button removed %v", removed)
	}
}
