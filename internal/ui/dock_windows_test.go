package ui

import (
	"fmt"
	"testing"
)

func testTask(name string, clicked *[]string) Row {
	return TaskItem(TaskSpec{Icon: IconScissors, Name: name, Detail: "Cutting clips into “Clips”", Tip: `D:\Videos\` + name, Busy: true,
		Tools: []Tool{
			{Icon: IconExternalLink, Tip: "Show the editor", OnClick: func() { *clicked = append(*clicked, "show "+name) }},
			{Icon: IconClose, Tip: "Stop editing", OnClick: func() { *clicked = append(*clicked, "stop "+name) }},
		}})
}

// layoutPopup lays rows out as the panel would, without a window.
func layoutPopup(rows []Row) *Popup {
	p := &Popup{scale: 1, rows: rows, hoverRow: -1, pressRow: -1, dropRow: -1}
	c := &canvas{scale: 1}
	p.width = c.px(popupWidthL)
	p.layout(c, false, c.px(popupMaxL))
	return p
}

// A long page with a dock: the docked rows sit at the bottom whatever the
// scroll position, the page scrolls further by the dock's height so its
// last row comes out from under it, and a point over the dock reaches the
// dock's rows, not the page's row scrolled under it.
func TestDockLayout(t *testing.T) {
	var clicked []string
	rows := []Row{Docked()} // empty: nothing is docked
	for range 30 {
		rows = append(rows, Button("Row", ButtonNormal, false, nil))
	}
	rows = append(rows, Docked(Section("Open in the editor"), testTask("a.mp4", &clicked), testTask("b.mkv", &clicked),
		Button("Stop editing all 2", ButtonNormal, false, nil)))
	p := layoutPopup(rows)

	const pad, dockH = popupPadL, dockPadL + 28 + 46 + 46 + 38 + dockPadL
	if p.height != popupMaxL || p.dockN != 4 || p.dockH != dockH {
		t.Fatalf("height %d, %d docked rows, dock %d px; want %d, 4, %d", p.height, p.dockN, p.dockH, popupMaxL, dockH)
	}
	view := p.viewBottom()
	n := p.dockStart()
	if n != 30 {
		t.Fatalf("dock starts at slot %d", n)
	}
	if want := int32(30*38) + 2*pad + dockH - popupMaxL; p.maxScroll() != want {
		t.Errorf("maxScroll = %d, want %d", p.maxScroll(), want)
	}
	for _, scroll := range []int32{0, 200, p.maxScroll()} {
		p.scroll = scroll
		if r := p.rowRect(n); r.Top != view+dockPadL {
			t.Errorf("scroll %d: first docked row at %d, want %d", scroll, r.Top, view+dockPadL)
		}
		if r := p.rowRect(len(p.slots) - 1); r.Bottom != p.height-dockPadL {
			t.Errorf("scroll %d: last docked row ends at %d, want %d", scroll, r.Bottom, p.height-dockPadL)
		}
	}
	// Scrolled to the end, the page's last row ends above the dock.
	p.scroll = p.maxScroll()
	if r := p.rowRect(n - 1); r.Bottom != view-pad {
		t.Errorf("last page row ends at %d, want %d (above the dock at %d)", r.Bottom, view-pad, view)
	}

	// At the top, page rows lie under the dock; the dock gets the point.
	p.scroll = 0
	y := view + dockPadL + 28 + 46 + 20 // in b.mkv's row
	if row, seg := p.hitTest(100, y); row != n+2 || seg != 0 {
		t.Errorf("hitTest over the dock = (%d, %d), want (%d, 0)", row, seg, n+2)
	}
	if row := p.rowAt(y); row != n+2 {
		t.Errorf("rowAt over the dock = %d, want %d", row, n+2)
	}
	if row, _ := p.hitTest(100, view-20); row < 0 || row >= n {
		t.Errorf("hitTest above the dock = %d, want a page row", row)
	}
	// A click on b.mkv's stop button.
	segs := p.slots[n+2].row.segments(&canvas{scale: 1}, p.rowRect(n+2))
	stop := segs[2]
	if row, seg := p.hitTest(stop.Left+2, stop.Top+2); row != n+2 || seg != 2 {
		t.Fatalf("hitTest on stop = (%d, %d)", row, seg)
	}
	p.slots[n+2].row.click(2)
	if len(clicked) != 1 || clicked[0] != "stop b.mkv" {
		t.Errorf("clicks reached %v", clicked)
	}
}

// A page shorter than the panel: its Flex row grows so that the panel keeps
// its minimum height with the dock, and its last row ends above the dock.
func TestDockShortPage(t *testing.T) {
	p := layoutPopup([]Row{Flex(), Button("Last", ButtonNormal, false, nil), Docked(Button("Stop", ButtonNormal, false, nil))})
	if p.height != popupMinL || p.maxScroll() != 0 {
		t.Fatalf("height %d, maxScroll %d; want %d, 0", p.height, p.maxScroll(), popupMinL)
	}
	if r := p.rowRect(1); r.Bottom != p.viewBottom()-popupPadL {
		t.Errorf("last page row ends at %d, want %d", r.Bottom, p.viewBottom()-popupPadL)
	}
	if r := p.rowRect(2); r.Bottom != p.height-dockPadL {
		t.Errorf("docked row ends at %d, want %d", r.Bottom, p.height-dockPadL)
	}

	// Without a dock, nothing changes: the page runs to the bottom.
	p = layoutPopup([]Row{Flex(), Button("Last", ButtonNormal, false, nil)})
	if p.dockN != 0 || p.viewBottom() != p.height || p.rowRect(1).Bottom != p.height-popupPadL {
		t.Errorf("without a dock: %d docked, view to %d of %d, last row to %d", p.dockN, p.viewBottom(), p.height, p.rowRect(1).Bottom)
	}
}

// A task's tools are squares at its right end with their own tooltips; its
// text has the tooltip of its own (the path) and does nothing on a click.
func TestTaskItem(t *testing.T) {
	c := &canvas{scale: 1}
	r := rect{0, 0, 340, 46}
	var clicked []string
	row := testTask("a.mp4", &clicked)
	segs := row.segments(c, r)
	if len(segs) != 3 || segs[0].Right > segs[1].Left || segs[1].Right > segs[2].Left || segs[1].w() != segs[1].h() || segs[2].Right != 340-padX {
		t.Fatalf("segments = %v", segs)
	}
	row.click(0)
	row.click(1)
	if fmt.Sprint(clicked) != "[show a.mp4]" {
		t.Errorf("clicks reached %v", clicked)
	}
	tip := row.(tipper)
	if tip.tip(c, r, 0) != `D:\Videos\a.mp4` || tip.tip(c, r, 2) != "Stop editing" {
		t.Errorf("tips %q, %q", tip.tip(c, r, 0), tip.tip(c, r, 2))
	}
	if segs := TaskItem(TaskSpec{Name: "a"}).segments(c, r); len(segs) != 1 || segs[0].w() != 0 {
		t.Errorf("a task without a tip or tools has segments %v", segs)
	}
}

// TestDockRender paints the library page with files open in the editor:
// at the top, with folders under the dock, and scrolled to the end, with
// the page's last rows above it and a stop button under the pointer.
// RESOUND_RENDER_DIR saves PNGs.
func TestDockRender(t *testing.T) {
	var clicked []string
	tools := []Tool{{Icon: IconScissors, Tip: "Edit"}, {Icon: IconPencil, Tip: "Rename"}, {Icon: IconTrash, Tip: "Delete", Danger: true}}
	rows := []Row{HeaderTool("Sound library", func() {}, IconRefresh, "Refresh", func() {}), Section("Folders")}
	for i := range 8 {
		rows = append(rows, FolderDrop(FolderSpec{Name: fmt.Sprintf("Folder %d", i+1), Detail: "12 sounds", Depth: min(i, 1), OnDrop: func(DropData) {}}))
	}
	rows = append(rows,
		GroupStart(),
		FolderDrop(FolderSpec{Name: "Clips", Detail: "3 sounds", Note: "Editing in your browser…", Depth: 1, Active: true, Expanded: true, OnClick: func() {}, OnDrop: func(DropData) {}}),
		GroupEnd(),
		SoundItem(SoundSpec{Name: "Bruh", Depth: 2, Tools: tools}),
		SoundItem(SoundSpec{Name: "Vine boom with a name too long for the row", Depth: 2, Note: "Editing…",
			Tools: []Tool{{Icon: IconExternalLink, Tip: "Show the editor"}}}),
		Section("Loudness"),
		Toggle("Even out the loudness of sounds", true, nil),
		Docked(Section("Open in the editor"), testTask("Funny moment.mp4", &clicked),
			TaskItem(TaskSpec{Icon: IconScissors, Name: "Vine boom.mp3", Detail: "Opening the editor…", Tools: []Tool{{Icon: IconExternalLink}, {Icon: IconClose}}}),
			Button("Stop editing all 2", ButtonNormal, false, nil)),
	)
	for _, scale := range []float64{1, 1.5} {
		for _, end := range []bool{false, true} {
			m := &canvas{scale: scale}
			w, h := m.px(popupWidthL), m.px(popupMaxL)
			img := renderCanvas(t, w, h, scale, func(c *canvas) {
				p := &Popup{scale: scale, rows: rows, fonts: c.fonts, hoverRow: -1, pressRow: -1, dropRow: -1, width: w}
				p.layout(c, true, h)
				if p.height != h {
					t.Fatalf("height %d, want %d", p.height, h)
				}
				if end {
					p.scroll = p.maxScroll()
					p.hoverRow, p.hoverSeg = p.dockStart()+1, 2
				}
				p.paintTo(c)
			})
			name := "library-dock"
			if end {
				name += "-end"
			}
			if scale != 1 {
				name += "@1.5x"
			}
			savePNG(t, img, name)
		}
	}
}
