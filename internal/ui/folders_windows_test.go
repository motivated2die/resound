package ui

import "testing"

// TestLibraryRowsRender paints the library page's rows: the import guide,
// a folder with nested subfolders (one being imported into, one with
// something dragged over it, one showing "Added"), the name field of an
// imported sound with its buttons, and the home page's section link.
// RESOUND_RENDER_DIR saves a PNG.
func TestLibraryRowsRender(t *testing.T) {
	dropped := false
	rows := []Row{
		SectionLink("Sound library", "Import buttons", "Download sounds", func() {}),
		Section("Import sounds"),
		ImportGuide("Find a sound on Myinstants, then drag its name onto a folder below. Resound downloads it there."),
		Section("Folders"),
		FolderDrop(FolderSpec{Name: "Memes", Detail: "42 sounds · 3 tabs", ActionIcon: IconTrash, ActionTip: "Remove", OnAction: func() {}, OnClick: func() {}, OnDrop: func(DropData) { dropped = true }}),
		FolderDrop(FolderSpec{Name: "Anime", Detail: "12 sounds", Depth: 1, OnDrop: func(DropData) {}}),
		FolderDrop(FolderSpec{Name: "Naruto", Detail: "Empty", Depth: 2, OnDrop: func(DropData) {}}),
		GroupStart(),
		FolderDrop(FolderSpec{Name: "Reactions", Note: "Name the new sound", Depth: 1, Active: true, OnDrop: func(DropData) {}}),
		Paragraph("Already in your library: “Vine boom” in Memes.", ToneWarning),
		TextField("f", "FAHHHHHHHHHHHHHH", 80, nil, nil),
		Buttons(Action{Label: "Discard", Icon: IconTrash, Style: ButtonDanger}, Action{Label: "Preview", Icon: IconPlay}, Action{Label: "Save", Style: ButtonPrimary}),
		GroupEnd(),
		FolderDrop(FolderSpec{Name: "Games", Detail: "7 sounds", Note: "Added “Mario jump”", Depth: 1, OnDrop: func(DropData) {}}),
		FolderDrop(FolderSpec{Name: "Classics", Detail: "Empty", Depth: 1, OnDrop: func(DropData) {}}),
	}
	states := map[int]rowState{
		0:  {hover: 0, press: -1, open: -1},
		4:  {hover: 1, press: -1, open: -1},
		14: {hover: -1, press: -1, open: -1, drop: true},
	}
	for _, scale := range []float64{1, 1.5} {
		img := renderRows(t, rows, scale, states)
		if scale == 1.5 {
			savePNG(t, img, "library-rows")
		}
	}

	d, ok := rows[4].(dropper)
	if !ok || d.dropHandler() == nil {
		t.Fatal("FolderDrop is not a drop field")
	}
	d.dropHandler()(DropData{Links: []string{"https://example.org/a.mp3"}})
	if !dropped {
		t.Error("drop handler not called")
	}
	if _, ok := rows[0].(dropper); ok {
		t.Error("SectionLink should not take drops")
	}
}

// The folder row's trash button is square, as tall as the field, and has
// its own tooltip; a subfolder without OnClick has no clickable field.
func TestFolderDropSegments(t *testing.T) {
	c := &canvas{scale: 1}
	r := rect{0, 0, 340, 42}
	root := FolderDrop(FolderSpec{Name: "Memes", ActionIcon: IconTrash, ActionTip: "Remove", OnClick: func() {}})
	segs := root.segments(c, r)
	if len(segs) != 2 || segs[0].w() == 0 || segs[0].Right >= segs[1].Left {
		t.Fatalf("root segments = %v", segs)
	}
	if segs[1].h() != segs[0].h() || segs[1].w() != segs[1].h() {
		t.Fatalf("action %v should be square and as tall as the field %v", segs[1], segs[0])
	}
	if tip := root.(tipper).tip(c, r, 1); tip != "Remove" {
		t.Fatalf("action tip = %q", tip)
	}
	sub := FolderDrop(FolderSpec{Name: "Anime", Depth: 1})
	if segs := sub.segments(c, r); segs[0].w() != 0 || segs[1].w() != 0 {
		t.Fatalf("sub segments = %v", segs)
	}
}
