package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

// renderRows paints rows into an offscreen bitmap the panel's width, the
// way Popup.paint does, and returns it. states gives a row's state (nil: no
// hover, press or open list).
func renderRows(t *testing.T, rows []Row, scale float64, states map[int]rowState) *image.RGBA {
	t.Helper()
	f := newFonts(scale)
	defer f.destroy()
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		t.Skip("no device context")
	}
	defer procDeleteDC.Call(dc)
	c := &canvas{dc: dc, scale: scale, fonts: f}
	w := c.px(popupWidthL)
	var h int32
	heights := make([]int32, len(rows))
	for i, r := range rows {
		heights[i] = c.px(r.height())
		if m, ok := r.(measurer); ok {
			heights[i] = m.measure(c, w)
		}
		h += heights[i]
	}
	h += 2 * c.px(popupPadL)

	type bitmapInfoHeader struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
	}
	hdr := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 {
		t.Fatal("CreateDIBSection failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(dc, bmp)
	defer procSelectObject.Call(dc, old)

	c.fill(rect{0, 0, w, h}, colBackground)
	y := c.px(popupPadL)
	for i, r := range rows {
		st, ok := states[i]
		if !ok {
			st = rowState{hover: -1, press: -1, open: -1}
		}
		r.paint(c, rect{0, y, w, y + heights[i]}, st)
		y += heights[i]
	}

	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	src := unsafe.Slice((*uint8)(bits), int(w*h*4))
	for i := 0; i < len(src); i += 4 { // BGRA → RGBA
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 255
	}
	return img
}

// savePNG writes img to $RESOUND_RENDER_DIR/name.png when that is set, to
// look at the panel without running the app.
func savePNG(t *testing.T, img image.Image, name string) {
	dir := os.Getenv("RESOUND_RENDER_DIR")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func level(v float64) func() float64 { return func() float64 { return v } }

func testFlow() AudioFlow {
	return AudioFlow{
		Mic:    FlowDevice{ID: "mic", Caption: "Your microphone", Options: []Option{{Label: "Automatic: Headset Microphone (Realtek)"}}, Selected: 0, Level: level(0.62)},
		Sounds: FlowSounds{Caption: "Soundboard", Status: "Playing", Playing: true, Level: level(0.8)},
		Cable:  FlowCable{Caption: "Others hear", Value: "100%", Name: "CABLE Output", Info: "why", Level: level(0.85), Volume: &FlowVolume{Value: 100}},
		You: FlowDevice{ID: "monitor", Caption: "You hear", Value: "80%", Options: []Option{{Label: "System default"}},
			Level: level(0.55), Volume: &FlowVolume{Value: 80}},
		MicPipe:   Pipe{State: PipeOn, Level: level(0.62)},
		CablePipe: Pipe{State: PipeOn, Level: level(0.8)},
		CableLine: Pipe{State: PipeOn, Level: level(0.85)},
		YouPipe:   Pipe{State: PipeOn, Level: level(0.55)},
	}
}

func TestFlowRender(t *testing.T) {
	alert := func(label string, tone Tone) *Badge {
		return &Badge{Icon: IconAlert, Label: label, Tone: tone, Tip: "why", OnClick: func() {}}
	}
	setup := testFlow()
	setup.Sounds = FlowSounds{Caption: "Soundboard"}
	setup.Mic.Level = level(0.4)
	setup.Cable.Level, setup.You.Level = level(0), level(0)
	setup.MicPipe = Pipe{State: PipeBroken, Badge: alert("Not routed", ToneWarning)}
	setup.CablePipe = Pipe{State: PipeOn, Level: level(0)}
	setup.CableLine = Pipe{State: PipeOn, Level: level(0)}
	setup.YouPipe = Pipe{State: PipeOn, Level: level(0)}

	preview := setup
	preview.MicPipe = Pipe{State: PipeOn, Level: level(0)}
	preview.Sounds = FlowSounds{Caption: "Soundboard", Status: "Preview", Playing: true, Level: level(0.7)}
	preview.YouPipe = Pipe{State: PipeOn, Level: level(0.7)}
	preview.You.Level = level(0.7)

	held := testFlow()
	held.MicPipe = Pipe{State: PipeHeld, Badge: &Badge{Icon: IconMicOff, Label: "Muted for sounds", Tip: "why"}}
	held.Mic.Level = level(0)
	held.CableLine.Badge = &Badge{Icon: IconKeyboard, Label: "Holding F13", Tone: ToneGood, Tip: "why"}

	paused := testFlow()
	paused.Sounds.Status = "Paused"
	paused.CablePipe = Pipe{State: PipeHeld, Badge: &Badge{Icon: IconPause, Label: "Talking", Tip: "why"}}
	paused.YouPipe = Pipe{State: PipeHeld}

	missing := testFlow()
	missing.Sounds = FlowSounds{Caption: "Soundboard"}
	missing.MicPipe = Pipe{State: PipeOff}
	missing.CablePipe = Pipe{State: PipeOff}
	missing.CableLine = Pipe{State: PipeBroken, Badge: alert("VB-Cable missing", ToneWarning)}
	missing.YouPipe = Pipe{State: PipeBroken, Badge: alert("Output failed", ToneBad)}
	missing.Cable.Name, missing.Cable.Placeholder, missing.Cable.Level = "", "Not installed", level(0)

	for name, f := range map[string]AudioFlow{"flow-playing": testFlow(), "flow-setup": setup, "flow-preview": preview,
		"flow-held": held, "flow-paused": paused, "flow-missing": missing} {
		for _, scale := range []float64{1, 1.5} {
			img := renderRows(t, f.Rows(), scale, nil)
			if scale == 1.5 {
				name += "@1.5x"
			}
			savePNG(t, img, name)
			// The pipes join across rows: the mic lane is drawn in the last
			// pixel row of the mic row and the first of the next one.
			if f.MicPipe.State == PipeOn {
				c := &canvas{scale: scale}
				micH := c.px(flowMicRow{}.height())
				lane, _ := flowLanes(c, rect{0, 0, c.px(popupWidthL), 1})
				y := c.px(popupPadL) + micH
				bg := color.RGBA{0x1C, 0x1D, 0x22, 255}
				if img.RGBAAt(int(lane), int(y-1)) == bg || img.RGBAAt(int(lane), int(y)) == bg {
					t.Errorf("%s: mic lane doesn't join at x=%d y=%d", name, lane, y)
				}
			}
		}
	}
}

func TestFlowSegments(t *testing.T) {
	c := &canvas{scale: 1}
	r := rect{0, 0, popupWidthL, 200}
	f := testFlow()
	rows := f.Rows()

	// Segment indexes stay put whether or not a badge is shown.
	if n := len(rows[1].segments(c, r)); n != 3 {
		t.Errorf("sounds row: %d segments, want 3", n)
	}
	if n := len(rows[2].segments(c, r)); n != 2 {
		t.Errorf("cable row: %d segments, want 2", n)
	}
	for i, s := range append(rows[1].segments(c, r)[1:], rows[2].segments(c, r)...) {
		if s.w() != 0 {
			t.Errorf("badge segment %d without a badge is %v, want empty", i, s)
		}
	}

	// Outputs: the cable card and its info icon, the you field, then two
	// volume tracks.
	out := rows[3].(flowOutputsRow)
	if _, ok := out.field(segCableCard); ok {
		t.Error("the cable card is a dropdown")
	}
	if f, ok := out.field(segYouField); !ok || f.id != "monitor" {
		t.Errorf("you field = %q, %v; want monitor", f.id, ok)
	}
	if out.tip(c, r, segCableInfo) != "why" {
		t.Errorf("info tip = %q", out.tip(c, r, segCableInfo))
	}
	if out.dragIDAt(segYouField) != "" || out.dragIDAt(segYouVolume) != "flow-volume:you" {
		t.Errorf("drag ids: %q, %q", out.dragIDAt(segYouField), out.dragIDAt(segYouVolume))
	}

	var got []int
	f.You.Volume.OnChange = func(v int, done bool) { got = append(got, v) }
	out = f.Rows()[3].(flowOutputsRow)
	segs := out.segments(c, r)
	_, x0, x1 := trackRange(c, segs[segYouVolume])
	out.dragTo(c, r, "flow-volume:you", x0-40, false) // clamps to 0
	out.dragTo(c, r, "flow-volume:you", x1+40, false) // clamps to 100
	out.dragTo(c, r, "flow-volume:cable", x0, false)  // the other track: not this callback
	if len(got) != 2 || got[0] != 0 || got[1] != 100 {
		t.Errorf("volume drags = %v, want [0 100]", got)
	}
}
