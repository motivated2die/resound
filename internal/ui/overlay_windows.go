package ui

import (
	"fmt"
	"strings"
	"time"
	"unsafe"
)

// The overlay shows over games without ever taking the focus: a layered
// window (per-pixel alpha, drawn with UpdateLayeredWindow) that is topmost,
// a tool window (no taskbar button), never activated, and transparent to
// the mouse, so clicks reach the game. Its input (its key, Esc, the wheel)
// comes from the app's keyboard and mouse hooks (input.Watcher), not from
// the window. Like the panel, it can't show over exclusive fullscreen.
//
// A frame is drawn with Direct2D (d2d_windows.go) into a surface: bar,
// buttons and highlight, then the drop shadow beneath, in Go. Sliding in
// and out only moves the window and fades it; a frame is drawn again only
// when the highlight moves or flashes, or while Esc is held.
//
// It slides in from the screen's very edge, fading in as it moves away
// from it, and is clipped to the primary screen throughout: nothing of it
// is ever drawn on a screen next to it.

const (
	overlayClass   = "ResoundOverlay"
	overlayTimerID = 1
	overlayFrameMS = 10

	overlaySlideIn  = 130 * time.Millisecond
	overlaySlideOut = 100 * time.Millisecond
	overlayFlash    = 150 * time.Millisecond

	monitorDefaultToPrimary = 1
	swShowNoActivate        = 4
)

// Colors.
var (
	ovBarColor    = rgbaF(rgb(0x22, 0x24, 0x2A), 0.96)
	ovPillColor   = rgbaF(rgb(0x47, 0x4A, 0x55), 1)
	ovTextColor   = rgbaF(colText, 1)
	ovMutedColor  = rgbaF(colMuted, 1)
	ovFlashOK     = rgbaF(colAccent, 1)
	ovFlashOKText = rgbaF(colAccentText, 1)
	ovFlashBad    = rgbaF(colDanger, 1)
	ovHoldColor   = rgbaF(rgb(0x7A, 0x1F, 0x36), 0.97) // the bar while Esc is held to stop
)

func mixF(a, b colorF, t float64) colorF {
	u := float32(max(0, min(1, t)))
	return colorF{a.R + (b.R-a.R)*u, a.G + (b.G-a.G)*u, a.B + (b.B-a.B)*u, a.A + (b.A-a.A)*u}
}

// overlayScene is what one frame shows.
type overlayScene struct {
	items   []OverlayItem
	sel     int
	left    bool    // the bar at the left edge (the highlight reaches right)
	scale   float64 // device pixels per logical pixel
	maxH    int     // the bar's logical height limit
	nameMax int     // widest name, logical
	flash   int     // 0, flashOK or flashBad: the highlight confirms a play
	hold    float64 // how far Esc has been held to stop all sounds, 0..1
	empty   string  // the message shown without favorites
	shadow  overlayShadow
	// hint replaces the highlighted name, with arrows above and below it
	// (the panel's picture: "SCROLL").
	hint string
}

const (
	flashOK = 1 + iota
	flashBad
)

// renderOverlay draws a scene into a new surface.
func renderOverlay(sc overlayScene) (*surface, error) {
	g, err := getD2D()
	if err != nil {
		return nil, err
	}
	s := float32(sc.scale)
	px := func(v int) float32 { return float32(v) * s }
	nameFont := fontSpec{family: "Segoe UI", size: 17 * s, weight: 700}
	if len(sc.items) == 0 {
		return renderOverlayEmpty(g, sc, nameFont)
	}

	n := len(sc.items)
	vis := overlayVisible(n, sc.maxH)
	sel := clampSelection(sc.sel, n)
	first := overlayFirst(n, vis, sel)
	reach := ovNameGap + sc.nameMax + ovNameLead - ovInset // past the bar's inner edge
	// The screen edge's side is padded by the bar's distance to it; the
	// shadow beyond that would fall off the screen anyway.
	pad := sc.shadow.pad()
	w := int32(px(ovEdge + ovBar + reach + pad))
	h := int32(px(2*pad + overlayBarHeight(vis)))
	surf, err := newSurface(w, h)
	if err != nil {
		return nil, err
	}
	barL := px(ovEdge)
	if !sc.left {
		barL = float32(w) - px(ovEdge+ovBar)
	}
	bar := rectF{barL, px(pad), barL + px(ovBar), px(pad + overlayBarHeight(vis))}
	barColor := mixF(ovBarColor, ovHoldColor, sc.hold)
	cx := barL + px(ovBar)/2
	cyOf := func(k int) float32 { return bar.T + px(ovInset) + float32(k)*px(ovDot+ovGap) + px(ovDot)/2 }

	p, err := g.begin(surf)
	if err != nil {
		surf.free()
		return nil, err
	}
	p.fillRound(bar, px(ovBar)/2, barColor)

	// The highlight: a pill around the button reaching inwards, with the
	// name (its first nameChars characters) beside the icon.
	var pill rectF
	if k := sel - first; k >= 0 && k < vis {
		cy := cyOf(k)
		label := TruncateName(sc.items[sel].Name, ovNameChars)
		if sc.hint != "" {
			label = sc.hint
		}
		nameW := min(g.measure(label, nameFont), px(sc.nameMax))
		pw := px(ovPill)/2 + px(ovDot)/2 + px(ovNameGap) + nameW + px(ovNameLead)
		top, bottom := cy-px(ovPill)/2, cy+px(ovPill)/2
		var name rectF
		if sc.left {
			pill = rectF{cx - px(ovPill)/2, top, cx - px(ovPill)/2 + pw, bottom}
			name = rectF{cx + px(ovDot)/2 + px(ovNameGap), top, pill.R - px(ovNameLead) + 2, bottom}
		} else {
			pill = rectF{cx + px(ovPill)/2 - pw, top, cx + px(ovPill)/2, bottom}
			name = rectF{pill.L + px(ovNameLead) - 2, top, cx - px(ovDot)/2 - px(ovNameGap), bottom}
		}
		pillColor, textColor := ovPillColor, ovTextColor
		switch sc.flash {
		case flashOK:
			pillColor, textColor = ovFlashOK, ovFlashOKText
		case flashBad:
			pillColor = ovFlashBad
		}
		p.fillRound(pill, px(ovPill)/2, pillColor)
		if name.R > name.L {
			p.text(label, name, nameFont, textColor)
		}
		if sc.hint != "" { // arrows over and under the hint: the wheel moves it
			arrow := fontSpec{family: "Segoe UI Symbol", size: 19 * s, weight: 400, align: alignCenter}
			mid := name.L + min(g.measure(label, nameFont), name.R-name.L)/2 // over the (left-aligned) label
			a := rectF{mid - px(16), top - px(28), mid + px(16), top - px(4)}
			p.text("▲", a, arrow, ovTextColor)
			a.T, a.B = bottom+px(4), bottom+px(28)
			p.text("▼", a, arrow, ovTextColor)
		}
	}
	if err := p.end(); err != nil {
		surf.free()
		return nil, err
	}
	// The buttons on top, from a layer drawn once per list and position:
	// laying out emoji is most of a frame's work, and frames are drawn
	// anew while Esc is held.
	if buttons := overlayButtons(g, sc.items[first:first+vis], bar, s); buttons != nil {
		buttons.over(surf, int32(bar.L), int32(bar.T))
	}

	// More buttons above or below: the edge buttons fade into the bar.
	fade := px(ovInset) + px(ovDot)*0.95
	if first > 0 {
		fadeInto(surf, bar, bar.T, bar.T+fade, barColor)
	}
	if first+vis < n {
		fadeInto(surf, bar, bar.B, bar.B-fade, barColor)
	}
	shadow := newShadowLayer(surf)
	shadow.add(bar, px(ovBar)/2, s, sc.shadow)
	if pill.R > pill.L {
		shadow.add(pill, px(ovPill)/2, s, sc.shadow)
	}
	shadow.under(surf)
	return surf, nil
}

// renderOverlayEmpty draws the hint shown without favorites: a pill at the
// edge with a short message.
func renderOverlayEmpty(g *d2d, sc overlayScene, font fontSpec) (*surface, error) {
	s := float32(sc.scale)
	px := func(v int) float32 { return float32(v) * s }
	textW := g.measure(sc.empty, font)
	pad := sc.shadow.pad()
	w := int32(px(ovEdge+pad+2*ovNameLead) + textW)
	h := int32(px(2*pad + ovPill))
	surf, err := newSurface(w, h)
	if err != nil {
		return nil, err
	}
	pill := rectF{px(ovEdge), px(pad), float32(w) - px(pad), float32(h) - px(pad)}
	if !sc.left {
		pill.L, pill.R = px(pad), float32(w)-px(ovEdge)
	}
	p, err := g.begin(surf)
	if err != nil {
		surf.free()
		return nil, err
	}
	p.fillRound(pill, px(ovPill)/2, mixF(ovBarColor, ovHoldColor, sc.hold))
	p.text(sc.empty, rectF{pill.L + px(ovNameLead), pill.T, pill.R - px(ovNameLead) + 4, pill.B}, font, ovMutedColor)
	if err := p.end(); err != nil {
		surf.free()
		return nil, err
	}
	shadow := newShadowLayer(surf)
	shadow.add(pill, px(ovPill)/2, s, sc.shadow)
	shadow.under(surf)
	return surf, nil
}

// buttonsCache holds the last buttons layer.
var buttonsCache struct {
	key  string
	surf *surface
}

// overlayButtons returns a bar-sized layer with the buttons of items.
func overlayButtons(g *d2d, items []OverlayItem, bar rectF, s float32) *surface {
	w, h := int32(bar.R-bar.L+0.5), int32(bar.B-bar.T+0.5)
	var key strings.Builder
	fmt.Fprintf(&key, "%d|%d|%.3f", w, h, s)
	for _, it := range items {
		fmt.Fprintf(&key, "|%s\x00%s\x00%s", it.Name, it.Emoji, it.Color)
	}
	if buttonsCache.surf != nil && buttonsCache.key == key.String() {
		return buttonsCache.surf
	}
	surf, err := newSurface(w, h)
	if err != nil {
		return nil
	}
	p, err := g.begin(surf)
	if err != nil {
		surf.free()
		return nil
	}
	px := func(v int) float32 { return float32(v) * s }
	for k, it := range items {
		cy := px(ovInset) + float32(k)*px(ovDot+ovGap) + px(ovDot)/2
		paintOverlayButton(p, it, float32(w)/2, cy, px(ovDot)/2, s)
	}
	if err := p.end(); err != nil {
		surf.free()
		return nil
	}
	buttonsCache.surf.free()
	buttonsCache.key, buttonsCache.surf = key.String(), surf
	return surf
}

// over composites the surface (premultiplied) over dst at x, y.
func (s *surface) over(dst *surface, x, y int32) {
	for row := range s.h {
		ty := y + row
		if ty < 0 || ty >= dst.h {
			continue
		}
		for col := range s.w {
			tx := x + col
			if tx < 0 || tx >= dst.w {
				continue
			}
			i := 4 * (int(row)*int(s.w) + int(col))
			a := uint32(s.bits[i+3])
			if a == 0 {
				continue
			}
			j := 4 * (int(ty)*int(dst.w) + int(tx))
			for c := range 4 {
				dst.bits[j+c] = uint8(uint32(s.bits[i+c]) + uint32(dst.bits[j+c])*(255-a)/255)
			}
		}
	}
}

// paintOverlayButton draws a round button: the sound's color with its emoji
// or the first letters of its name.
func paintOverlayButton(p *painter, it OverlayItem, cx, cy, r, s float32) {
	p.fillCircle(cx, cy, r, rgbaF(overlayColor(it.Color), 1))
	box := rectF{cx - r - 4, cy - r, cx + r + 4, cy + r}
	if it.Emoji != "" {
		p.text(it.Emoji, box, fontSpec{family: "Segoe UI Emoji", size: 22 * s, weight: 400, align: alignCenter}, ovTextColor)
		return
	}
	p.text(Initials(it.Name), box, fontSpec{family: "Segoe UI", size: 17 * s, weight: 600, align: alignCenter}, ovTextColor)
}

// fadeInto blends the surface's pixels inside bar's columns towards the
// bar's color, fully at y0 and not at all at y1 (either order), keeping
// their alpha: buttons cut off at the bar's end fade into it.
func fadeInto(surf *surface, bar rectF, y0, y1 float32, c colorF) {
	x0, x1 := max(0, int32(bar.L)), min(surf.w, int32(bar.R+1))
	lo, hi := min(y0, y1), max(y0, y1)
	for y := max(0, int32(lo)); y < min(surf.h, int32(hi+1)); y++ {
		t := (float32(y) - y0) / (y1 - y0) // 0 at y0, 1 at y1
		t = max(0, min(1, t))
		keep := 0.08 + 0.92*t*t
		for x := x0; x < x1; x++ {
			i := (int(y)*int(surf.w) + int(x)) * 4
			a := float32(surf.bits[i+3]) / 255
			for ch, v := range [3]float32{c.B, c.G, c.R} {
				src := float32(surf.bits[i+ch])
				surf.bits[i+ch] = uint8(src*keep + v*255*a*(1-keep) + 0.5)
			}
		}
	}
}

// shadowLayer collects the drop shadows of the overlay's shapes (the
// panel's blur, shadow.go) and puts them beneath what was drawn.
type shadowLayer struct {
	w, h  int32
	alpha []uint8
}

func newShadowLayer(surf *surface) *shadowLayer {
	return &shadowLayer{w: surf.w, h: surf.h, alpha: make([]uint8, int(surf.w)*int(surf.h))}
}

// shadowKey identifies a blurred mask; frames often reuse them (the bar's
// while Esc is held, the highlight's while it moves).
type shadowKey struct {
	w, h, pad, blur, off, corner int
	opacity                      float64
}

type shadowMasks struct {
	mask   []uint8
	cw, ch int
}

var shadowCache = map[shadowKey]shadowMasks{}

// add casts the shadow of a rounded rectangle (s: device pixels per
// logical pixel).
func (l *shadowLayer) add(r rectF, radius, s float32, spec overlayShadow) {
	px := func(v int) int { return int(float32(v)*s + 0.5) }
	blur, off, spread := px(spec.blur), px(spec.offY), px(spec.spread)
	r = rectF{r.L - float32(spread), r.T - float32(spread), r.R + float32(spread), r.B + float32(spread)}
	pad := blur + off
	k := shadowKey{int(r.R - r.L + 0.5), int(r.B - r.T + 0.5), pad, blur, off, int(radius+0.5) + spread, spec.opacity}
	m, ok := shadowCache[k]
	if !ok {
		if len(shadowCache) > 32 {
			clear(shadowCache)
		}
		m.mask, m.cw, m.ch = shadowMask(k.w, k.h, pad, blur, off, k.corner, spec.opacity)
		limit := uint8(spec.maxAlpha*255 + 0.5)
		for i, a := range m.mask {
			m.mask[i] = min(a, limit)
		}
		shadowCache[k] = m
	}
	mask, cw, ch := m.mask, m.cw, m.ch
	ox, oy := int(r.L+0.5)-pad, int(r.T+0.5)-pad
	for y := range ch {
		ty := oy + y
		if ty < 0 || ty >= int(l.h) {
			continue
		}
		for x := range cw {
			tx := ox + x
			if tx < 0 || tx >= int(l.w) {
				continue
			}
			i := ty*int(l.w) + tx
			l.alpha[i] = max(l.alpha[i], mask[y*cw+x])
		}
	}
}

// under composites the shadow beneath the surface's premultiplied pixels.
func (l *shadowLayer) under(surf *surface) {
	for i, sa := range l.alpha {
		if sa == 0 {
			continue
		}
		a := uint32(surf.bits[4*i+3])
		surf.bits[4*i+3] = uint8(a + uint32(sa)*(255-a)/255)
	}
}

// primaryMonitor returns the primary screen's rectangle and DPI scale.
func primaryMonitor() (rect, float64) {
	mon, _, _ := procMonitorFromPoint.Call(0, monitorDefaultToPrimary)
	mi := monitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	scale := 1.0
	if procGetDpiForMonitor.Find() == nil {
		var dx, dy uint32
		if r, _, _ := procGetDpiForMonitor.Call(mon, mdtEffectiveDpi, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx > 0 {
			scale = float64(dx) / 96
		}
	}
	return mi.Monitor, scale
}

// Overlay is the in-game overlay window. Its methods run on the UI thread.
type Overlay struct {
	*layered
	scene     overlayScene
	flashAt   time.Time // when the confirming flash began
	holdStart time.Time // when Esc went down to stop all sounds; zero = not held
	holdFor   time.Duration
}

// NewOverlay creates the overlay window (hidden).
func NewOverlay() (*Overlay, error) {
	o := &Overlay{}
	l, err := newLayered(func() { o.tick() })
	if err != nil {
		return nil, err
	}
	o.layered = l
	l.place = func(mon rect, w, h int32, p float64) (dst, src point, cw, ch int32) {
		return overlayPlacement(mon, w, h, o.scene.left, int32(float64(ovEdge)*o.scene.scale), p)
	}
	return o, nil
}

// Show slides the overlay in with items, sel highlighted, at the left or
// right edge of the primary screen. empty is shown when there are no items.
func (o *Overlay) Show(items []OverlayItem, sel int, left bool, empty string) {
	now := time.Now()
	scale := o.open(now)
	o.scene = overlayScene{
		items: items, sel: clampSelection(sel, len(items)), left: left, scale: scale,
		maxH: int(float64(o.mon.h()) / scale * ovMaxShare), nameMax: ovNameMax, empty: empty,
		shadow: ovShadowGame,
	}
	o.holdStart = time.Time{}
	o.flashAt = time.Time{} // opened again during a flash
	o.render()
	o.present(o.slide(now))
	o.raise()
	o.startTimer()
}

// SetSelected moves the highlight, at once.
func (o *Overlay) SetSelected(sel int) {
	sel = clampSelection(sel, len(o.scene.items))
	if sel == o.scene.sel || !o.Visible() {
		return
	}
	o.scene.sel, o.scene.flash = sel, 0
	o.render()
	o.present(o.slide(time.Now()))
}

// Confirm flashes the highlight (green when the sound plays, red when it
// can't), then slides the overlay out.
func (o *Overlay) Confirm(ok bool) {
	if !o.Visible() {
		return
	}
	o.scene.flash = flashOK
	if !ok {
		o.scene.flash = flashBad
	}
	o.flashAt = time.Now()
	o.render()
	o.present(o.slide(time.Now()))
	o.startTimer()
}

// HoldToStop starts tinting the bar towards red while Esc is held for d to
// stop all sounds; the app closes the overlay when the time is up or Esc is
// let go.
func (o *Overlay) HoldToStop(d time.Duration) {
	if !o.Visible() {
		return
	}
	o.holdStart, o.holdFor = time.Now(), d
	o.startTimer()
}

// Hide slides the overlay out; onHidden (if set) runs once it is gone.
func (o *Overlay) Hide(onHidden func()) {
	o.flashAt = time.Time{}
	o.layered.Hide(onHidden)
}

// tick advances the animations; the timer runs only while one does.
func (o *Overlay) tick() {
	now := time.Now()
	p, gone := o.advance(now)
	if gone {
		o.scene.flash = 0
		o.gone()
		return
	}
	if !o.flashAt.IsZero() && now.Sub(o.flashAt) >= overlayFlash {
		o.Hide(nil)
		p = o.slide(now)
	}
	hold := o.scene.hold
	if !o.holdStart.IsZero() {
		t := min(1, float64(now.Sub(o.holdStart))/float64(o.holdFor))
		hold = t * t // slow at first: a quick tap barely tints
	}
	if hold != o.scene.hold {
		o.scene.hold = hold
		o.render()
	}
	o.present(p)
	holding := !o.holdStart.IsZero() && hold < 1
	if o.phase == ovShown && o.flashAt.IsZero() && !holding {
		o.stopTimer()
	}
}

func (o *Overlay) render() {
	surf, _ := renderOverlay(o.scene) // on failure, keeps the last frame
	o.setFrame(surf)
}
