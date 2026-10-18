package ui

import "testing"

// TestLibraryRowsRender paints the library page's rows: the import guide,
// a folder with nested subfolders (one being imported into, one with
// something dragged over it, one showing "Added"), the name field of an
// imported sound with its buttons, a file open in the editor, a video
// dragged over a folder, an opened folder under the pointer with its
// sounds and their tools, and the home page's section link.
// RESOUND_RENDER_DIR saves a PNG.
func TestLibraryRowsRender(t *testing.T) {
	soundTools := []Tool{{Icon: IconScissors, Tip: "Edit"}, {Icon: IconPencil, Tip: "Rename"}, {Icon: IconTrash, Tip: "Delete", Danger: true}}
	dropped := false
	var hinted []string
	rows := []Row{
		HeaderTool("Sound library", func() {}, IconRefresh, "Refresh", func() {}),
		SectionLink("Sound library", "import sounds", "Download sounds", func() {}),
		Section("Import sounds"),
		ImportGuide("Find a sound on Myinstants and drag its name onto a folder below. Resound downloads it there. Sound files from Explorer work too.", "drag its name"),
		Section("Folders"),
		FolderDrop(FolderSpec{Name: "Memes", Detail: "42 sounds · 3 tabs", ActionIcon: IconTrash, ActionTip: "Remove", OnAction: func() {}, OnClick: func() {}, OnDrop: func(DropData) { dropped = true }}),
		FolderDrop(FolderSpec{Name: "Anime", Detail: "12 sounds", Depth: 1, OnDrop: func(DropData) {}}),
		FolderDrop(FolderSpec{Name: "Naruto", Detail: "Empty", Depth: 2, OnDrop: func(DropData) {}}),
		GroupStart(),
		FolderDrop(FolderSpec{Name: "Reactions", Note: "Name the new sound", Depth: 1, Active: true, OnDrop: func(DropData) {}}),
		Paragraph("Already in your library: “Vine boom” in Memes.", ToneWarning),
		TextField("f", "FAHHHHHHHHHHHHHH", 80, nil, nil),
		Buttons(Action{Label: "Myinstants"}, Action{Label: "Sound Buttons World"}),
		Buttons(Action{Label: "Discard", Icon: IconTrash, Style: ButtonDanger}, Action{Label: "Preview", Icon: IconPlay}, Action{Label: "Save", Style: ButtonPrimary}),
		GroupEnd(),
		FolderDrop(FolderSpec{Name: "Games", Detail: "7 sounds", Note: "Added “Mario jump”", Depth: 1, OnDrop: func(DropData) {}}),
		FolderDrop(FolderSpec{Name: "Classics", Detail: "Empty", Depth: 1, OnDrop: func(DropData) {}}),
		GroupStart(),
		FolderDrop(FolderSpec{Name: "Clips", Detail: "3 sounds", Note: "Editing in your browser…", Depth: 1, Active: true, OnDrop: func(DropData) {}}),
		Paragraph("“Funny moment.mp4” is open in the editor in your browser. Cut the part you want and press “Add to Resound”: each clip is saved here.", ToneMuted),
		Buttons(Action{Label: "Show editor", Icon: IconExternalLink}, Action{Label: "Stop editing"}),
		GroupEnd(),
		FolderDrop(FolderSpec{Name: "Videos", Detail: "Empty", Depth: 1, OnDrop: func(DropData) {}, DropHint: func(d DropData) string {
			hinted = d.Paths
			return "Drop to cut in the editor"
		}}),
		// An opened folder under the pointer, with its sounds.
		FolderDrop(FolderSpec{Name: "Soundboard", Detail: "3 sounds", Depth: 1, Expanded: true, OnClick: func() {}, OnOpen: func() {}, OnDrop: func(DropData) {}}),
		SoundItem(SoundSpec{Name: "Bruh", Depth: 2, Tools: soundTools}),
		SoundItem(SoundSpec{Name: "FAHHHHHHHHHHHHHH with a name too long for the row", Depth: 2, Tools: soundTools}),
		SoundItem(SoundSpec{Name: "Vine boom", Depth: 2, Note: "Editing in your browser…"}),
		FolderDrop(FolderSpec{Name: "Empty", Detail: "Empty", Depth: 1, Expanded: true, OnClick: func() {}, OnOpen: func() {}, OnDrop: func(DropData) {}}),
		SoundItem(SoundSpec{Name: "No sounds in this folder", Depth: 2, Muted: true}),
	}
	dragged := DropData{Paths: []string{`D:\Funny moment.mp4`}}
	states := map[int]rowState{
		0:  {hover: 1, press: -1, open: -1},
		1:  {hover: 0, press: -1, open: -1},
		5:  {hover: 1, press: -1, open: -1},
		15: {hover: -1, press: -1, open: -1, drop: true},
		22: {hover: -1, press: -1, open: -1, drop: true, dragged: &dragged},
		23: {hover: 2, press: -1, open: -1}, // over "Show in Explorer"
		24: {hover: 3, press: -1, open: -1}, // over the trash
	}
	for _, scale := range []float64{1, 1.5} {
		img := renderRows(t, rows, scale, states)
		if scale == 1.5 {
			savePNG(t, img, "library-rows")
		}
	}

	if len(hinted) != 1 {
		t.Errorf("DropHint got %v, want the dragged video", hinted)
	}

	d, ok := rows[5].(dropper)
	if !ok || d.dropHandler() == nil {
		t.Fatal("FolderDrop is not a drop field")
	}
	d.dropHandler()(DropData{Links: []string{"https://example.org/a.mp3"}})
	if !dropped {
		t.Error("drop handler not called")
	}
	if _, ok := rows[1].(dropper); ok {
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
	if len(segs) != 3 || segs[0].w() == 0 || segs[0].Right >= segs[1].Left || segs[2].w() != 0 {
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

	// "Show in Explorer" sits inside the field's right end; the field's
	// own segment stops where it starts, so a click there opens Explorer.
	opened, toggled := false, false
	withOpen := FolderDrop(FolderSpec{Name: "Memes", ActionIcon: IconTrash, OnClick: func() { toggled = true }, OnOpen: func() { opened = true }})
	segs = withOpen.segments(c, r)
	box, _, open := withOpen.(folderDropRow).geometry(c, r)
	if open.w() == 0 || open.Left <= box.Left || open.Right > box.Right || segs[0].Right != open.Left || segs[2] != open {
		t.Fatalf("segments with OnOpen = %v (field %v)", segs, box)
	}
	withOpen.click(2)
	withOpen.click(0)
	if !opened || !toggled {
		t.Errorf("clicks: open %v, field %v", opened, toggled)
	}
	if tip := withOpen.(tipper).tip(c, r, 2); tip == "" {
		t.Error("the Explorer button has no tooltip")
	}
}

// A sound's tools are squares at the row's right end, in order; their
// clicks and tooltips reach the right tool. A placeholder has none.
func TestSoundItemTools(t *testing.T) {
	c := &canvas{scale: 1}
	r := rect{0, 0, 340, 28}
	var clicked []string
	row := SoundItem(SoundSpec{Name: "Horn", Depth: 1, Tools: []Tool{
		{Icon: IconScissors, Tip: "Cut", OnClick: func() { clicked = append(clicked, "cut") }},
		{Icon: IconPencil, Tip: "Rename", OnClick: func() { clicked = append(clicked, "rename") }},
		{Icon: IconTrash, Tip: "Delete", Danger: true, OnClick: func() { clicked = append(clicked, "delete") }},
	}})
	segs := row.segments(c, r)
	if len(segs) != 4 || segs[0].Right > segs[1].Left || segs[1].Right > segs[2].Left || segs[2].Right > segs[3].Left || segs[3].w() != segs[3].h() {
		t.Fatalf("segments = %v", segs)
	}
	row.click(1)
	row.click(3)
	row.click(0) // the name does nothing
	if len(clicked) != 2 || clicked[0] != "cut" || clicked[1] != "delete" {
		t.Errorf("clicks reached %v", clicked)
	}
	if tip := row.(tipper).tip(c, r, 2); tip != "Rename" {
		t.Errorf("tool tip = %q", tip)
	}
	if segs := SoundItem(SoundSpec{Name: "No sounds", Muted: true}).segments(c, r); len(segs) != 1 || segs[0].w() != 0 {
		t.Errorf("placeholder segments = %v", segs)
	}
}
