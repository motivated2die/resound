package ui

import "testing"

// TestLibraryRowsRender paints the library page's rows: a folder, a
// subfolder with something dragged over it, the name field of an imported
// sound, and the home page's section link. RESOUND_RENDER_DIR saves a PNG.
func TestLibraryRowsRender(t *testing.T) {
	dropped := false
	rows := []Row{
		SectionLink("Sound library", "Import from Myinstants", func() {}),
		Section("Folders"),
		FolderDrop(FolderSpec{Name: "Memes", Detail: "42 sounds · 3 tabs", Action: "Remove", OnAction: func() {}, OnClick: func() {}, OnDrop: func(DropData) { dropped = true }}),
		FolderDrop(FolderSpec{Name: "Anime", Detail: "12 sounds", Sub: true, OnDrop: func(DropData) {}}),
		FolderDrop(FolderSpec{Name: "Reactions", Detail: "Empty", Sub: true, OnDrop: func(DropData) {}}),
		Text("Added to “Reactions”. Name it:", "", ToneGood),
		TextField("f", "FAHHHHHHHHHHHHHH", 80, nil, nil),
		Buttons(Action{Label: "Keep name"}, Action{Label: "Save name", Style: ButtonPrimary}),
		FolderDrop(FolderSpec{Name: "Games", Detail: "7 sounds", Sub: true, OnDrop: func(DropData) {}}),
	}
	states := map[int]rowState{
		0: {hover: 0, press: -1, open: -1},
		4: {hover: -1, press: -1, open: -1, drop: true},
		8: {hover: 0, press: -1, open: -1},
	}
	for _, scale := range []float64{1, 1.5} {
		img := renderRows(t, rows, scale, states)
		if scale == 1.5 {
			savePNG(t, img, "library-rows")
		}
	}

	d, ok := rows[2].(dropper)
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

// The folder row's Remove button and its field are separate segments; a
// subfolder without OnClick has no clickable field.
func TestFolderDropSegments(t *testing.T) {
	c := &canvas{scale: 1}
	r := rect{0, 0, 340, 42}
	root := FolderDrop(FolderSpec{Name: "Memes", Action: "Remove", OnClick: func() {}})
	segs := root.segments(c, r)
	if len(segs) != 2 || segs[0].w() == 0 || segs[1].w() == 0 || segs[0].Right >= segs[1].Left {
		t.Fatalf("root segments = %v", segs)
	}
	sub := FolderDrop(FolderSpec{Name: "Anime", Sub: true})
	if segs := sub.segments(c, r); segs[0].w() != 0 || segs[1].w() != 0 {
		t.Fatalf("sub segments = %v", segs)
	}
}
