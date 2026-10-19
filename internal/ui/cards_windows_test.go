package ui

import "testing"

func TestCardsGeometry(t *testing.T) {
	cr := PageCards(
		PageCard{Title: "Folders", Tool: IconRefresh, OnTool: func() {}},
		PageCard{Title: "Web app", Tool: IconExternalLink}, // disabled tool
	).(pageCardsRow)
	r := rect{0, 0, popupWidthL, int32(cr.height())}
	segs := cr.segments(testCanvas, r)
	if len(segs) != 2*pageCardSegs {
		t.Fatalf("segments = %v", segs)
	}
	left, right := segs[pageCardSegBox], segs[pageCardSegs+pageCardSegBox]
	if left.Left != int32(padX) || right.Right != r.Right-int32(padX) {
		t.Errorf("cards %v %v should line up with the panel's padding", left, right)
	}
	if left.w() != right.w() || right.Left-left.Right != int32(pageCardGapL) {
		t.Errorf("cards %v %v should be equally wide, %d apart", left, right, pageCardGapL)
	}
	if left.h() != int32(pageCardHeightL) || left.Bottom > r.Bottom {
		t.Errorf("card %v should be %d tall, inside the row %v", left, pageCardHeightL, r)
	}
	tool := segs[pageCardSegTool]
	if tool.Left < left.Left || tool.Right > left.Right || tool.Top < left.Top || tool.Bottom > left.Bottom {
		t.Errorf("tool %v should sit inside its card %v", tool, left)
	}
	if tool.Right-left.Left < left.w()/2 || tool.Top-left.Top > int32(pageCardPadL) {
		t.Errorf("tool %v should be in the card's top right corner", tool)
	}
	if segs[pageCardSegs+pageCardSegTool] != (rect{}) {
		t.Error("a disabled tool should have no segment")
	}
}

func TestCardsClick(t *testing.T) {
	var got []string
	cr := PageCards(
		PageCard{OnClick: func() { got = append(got, "lib") }, Tool: IconRefresh, OnTool: func() { got = append(got, "refresh") }},
		PageCard{OnClick: func() { got = append(got, "remote") }, Tool: IconExternalLink},
	).(pageCardsRow)
	for seg := -1; seg <= 2*pageCardSegs; seg++ {
		cr.click(seg)
	}
	if want := []string{"refresh", "lib", "remote"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("clicks = %v, want %v", got, want)
	}
}

// TestCardsRender draws the home page's cards between the brand header and
// the audio flow, plain and hovered; see savePNG.
func TestCardsRender(t *testing.T) {
	cards := PageCards(
		PageCard{Section: "Sound library", Title: "Folders", Detail: "157 sounds · 4 tabs", Tone: ToneGood,
			Art: IconPlaylist, OnClick: func() {}, Tool: IconRefresh, ToolTip: "Refresh", OnTool: func() {}},
		PageCard{Section: "Remote", Title: "Web app", Detail: "1 device paired", Tone: ToneGood,
			Art: IconLayoutGrid, OnClick: func() {}, Tool: IconExternalLink, ToolTip: "Open", OnTool: func() {}},
	)
	rows := append([]Row{Brand(nil, "RESOUND", "v0.7.0", "Ready", nil, nil), cards, Spacer(6)}, testFlow().Rows()...)
	for _, scale := range []float64{1, 1.5} {
		for name, st := range map[string]rowState{
			"":            {hover: -1, press: -1, open: -1},
			"-hover":      {hover: pageCardSegBox, press: -1, open: -1},
			"-hover-tool": {hover: pageCardSegs + pageCardSegTool, press: -1, open: -1},
		} {
			img := renderRows(t, rows, scale, map[int]rowState{1: st})
			if scale != 1 {
				name += "@1.5x"
			}
			savePNG(t, img, "home-cards"+name)
		}
	}
}
