package ui

import "testing"

// Geometry needs no device context: px only scales.
var testCanvas = &canvas{scale: 1}

func TestSliderDrag(t *testing.T) {
	var got []int
	var done []bool
	s := Slider("v", "Volume", "", 50, 100, 10, func(v int, d bool) { got, done = append(got, v), append(done, d) }).(sliderRow)
	r := rect{0, 0, popupWidthL, 56}
	_, _, _, x0, x1 := s.geometry(testCanvas, r)

	s.dragTo(testCanvas, r, x0-50, false) // left of the track clamps to 0
	s.dragTo(testCanvas, r, x1+50, false) // right of it to max
	s.dragTo(testCanvas, r, (x0+x1)/2, false)
	if want := []int{0, 100}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("drag values = %v, want %v (the middle is the current value, so no change)", got, want)
	}
	s.dragTo(testCanvas, r, (x0+x1)/2, true) // release reports even without a change
	if len(got) != 3 || got[2] != 50 || !done[2] {
		t.Fatalf("release = %v %v, want 50 done", got, done)
	}
}

func TestSliderButtonsSnapToStep(t *testing.T) {
	for _, tc := range []struct{ pos, seg, want int }{
		{47, 0, 40}, {47, 2, 50}, {50, 0, 40}, {50, 2, 60},
		{0, 0, 0}, {100, 2, 100}, {95, 2, 100}, {3, 0, 0},
	} {
		var got int
		s := Slider("v", "", "", tc.pos, 100, 10, func(v int, _ bool) { got = v }).(sliderRow)
		s.click(tc.seg)
		if got != tc.want {
			t.Errorf("value %d, segment %d: got %d, want %d", tc.pos, tc.seg, got, tc.want)
		}
	}
}

func TestButtonsSegments(t *testing.T) {
	clicked := -1
	b := Buttons(
		Action{Label: "A", OnClick: func() { clicked = 0 }},
		Action{Label: "B", Disabled: true},
		Action{Label: "C", Closes: true, OnClick: func() { clicked = 2 }},
	).(buttonsRow)
	r := rect{0, 0, popupWidthL, 38}
	segs := b.segments(testCanvas, r)
	if len(segs) != 3 || segs[1] != (rect{}) || segs[0].w() <= 0 || segs[2].Right != r.Right-int32(padX) {
		t.Fatalf("segments = %v", segs)
	}
	if !b.keepsOpenAt(0) || b.keepsOpenAt(2) {
		t.Fatal("only the closing action should close the popup")
	}
	b.click(2)
	if clicked != 2 {
		t.Fatalf("clicked = %d", clicked)
	}
}
