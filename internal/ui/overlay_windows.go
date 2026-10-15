package ui

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
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
// when the highlight changes or grows.

const (
	overlayClass   = "ResoundOverlay"
	overlayTimerID = 1
	overlayFrameMS = 10

	overlaySlideIn  = 130 * time.Millisecond
	overlaySlideOut = 100 * time.Millisecond
	overlayGrow     = 90 * time.Millisecond
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
)

// overlayScene is what one frame shows.
type overlayScene struct {
	items   []OverlayItem
	sel     int
	left    bool    // the bar at the left edge (the highlight grows right)
	scale   float64 // device pixels per logical pixel
	maxH    int     // the bar's logical height limit
	nameMax int     // widest name, logical
	grow    float64 // the highlight's growth, 0..1
	flash   int     // 0, flashOK or flashBad: the highlight confirms a play
	empty   string  // the message shown without favorites
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
	w := int32(px(2*ovPad + ovBar + reach))
	h := int32(px(2*ovPad + overlayBarHeight(vis)))
	surf, err := newSurface(w, h)
	if err != nil {
		return nil, err
	}
	barL := px(ovPad)
	if !sc.left {
		barL = float32(w) - px(ovPad+ovBar)
	}
	bar := rectF{barL, px(ovPad), barL + px(ovBar), px(ovPad + overlayBarHeight(vis))}
	cx := barL + px(ovBar)/2
	cyOf := func(k int) float32 { return bar.T + px(ovInset) + float32(k)*px(ovDot+ovGap) + px(ovDot)/2 }

	p, err := g.begin(surf)
	if err != nil {
		surf.free()
		return nil, err
	}
	p.fillRound(bar, px(ovBar)/2, ovBarColor)

	// The highlight: a pill around the button that grows inwards.
	var pill rectF
	if k := sel - first; k >= 0 && k < vis {
		cy := cyOf(k)
		item := sc.items[sel]
		nameW := min(g.measure(item.Name, nameFont), px(sc.nameMax))
		minW := px(ovPill)
		fullW := px(ovPill)/2 + px(ovDot)/2 + px(ovNameGap) + nameW + px(ovNameLead)
		pw := minW + (fullW-minW)*float32(sc.grow)
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
		if a := float32(max(0, (sc.grow-0.35)/0.65)); a > 0 && name.R > name.L {
			textColor.A = a
			p.text(item.Name, name, nameFont, textColor)
		}
	}
	for k := range vis {
		paintOverlayButton(p, sc.items[first+k], cx, cyOf(k), px(ovDot)/2, s)
	}
	if err := p.end(); err != nil {
		surf.free()
		return nil, err
	}

	// More buttons above or below: the edge buttons fade into the bar.
	fade := px(ovInset) + px(ovDot)*0.95
	if first > 0 {
		fadeInto(surf, bar, bar.T, bar.T+fade, ovBarColor)
	}
	if first+vis < n {
		fadeInto(surf, bar, bar.B, bar.B-fade, ovBarColor)
	}
	shadow := newShadowLayer(surf)
	shadow.add(bar, px(ovBar)/2, s)
	if pill.R > pill.L {
		shadow.add(pill, px(ovPill)/2, s)
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
	w := int32(px(2*ovPad+2*ovNameLead) + textW)
	h := int32(px(2*ovPad + ovPill))
	surf, err := newSurface(w, h)
	if err != nil {
		return nil, err
	}
	pill := rectF{px(ovPad), px(ovPad), float32(w) - px(ovPad), float32(h) - px(ovPad)}
	p, err := g.begin(surf)
	if err != nil {
		surf.free()
		return nil, err
	}
	p.fillRound(pill, px(ovPill)/2, ovBarColor)
	p.text(sc.empty, rectF{pill.L + px(ovNameLead), pill.T, pill.R - px(ovNameLead) + 4, pill.B}, font, ovMutedColor)
	if err := p.end(); err != nil {
		surf.free()
		return nil, err
	}
	shadow := newShadowLayer(surf)
	shadow.add(pill, px(ovPill)/2, s)
	shadow.under(surf)
	return surf, nil
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

// add casts the shadow of a rounded rectangle.
func (l *shadowLayer) add(r rectF, radius, s float32) {
	blur, off := int(14*s+0.5), int(4*s+0.5)
	pad := blur + off
	w, h := int(r.R-r.L+0.5), int(r.B-r.T+0.5)
	mask, cw, ch := shadowMask(w, h, pad, blur, off, int(radius+0.5), 0.5)
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
	hwnd  uintptr
	scene overlayScene
	surf  *surface
	mon   rect

	phase      overlayPhase
	phaseStart time.Time
	growStart  time.Time
	flashAt    time.Time // when the confirming flash began
	timer      bool
	onHidden   func()
}

type overlayPhase int

const (
	ovHidden overlayPhase = iota
	ovShowing
	ovShown
	ovHiding
)

var overlayWindow *Overlay // for the window procedure

// NewOverlay creates the overlay window (hidden).
func NewOverlay() (*Overlay, error) {
	if _, err := getD2D(); err != nil {
		return nil, fmt.Errorf("drawing the overlay: %w", err)
	}
	if err := registerClass(overlayClass, 0, windows.NewCallback(overlayProc)); err != nil {
		return nil, err
	}
	h, err := createWindow(wsExLayered|wsExTransparent|wsExToolWindow|wsExTopmost|wsExNoActivate, wsPopup, overlayClass)
	if err != nil {
		return nil, err
	}
	o := &Overlay{hwnd: h}
	overlayWindow = o
	return o, nil
}

func overlayProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmTimer:
		if o := overlayWindow; o != nil && o.hwnd == hwnd {
			o.tick()
		}
		return 0
	case wmMouseActivate:
		return maNoActivate
	}
	return defWindowProc(hwnd, m, w, l)
}

// Visible reports whether the overlay is showing (also while it slides).
func (o *Overlay) Visible() bool { return o.phase != ovHidden && o.phase != ovHiding }

// Show slides the overlay in with items, sel highlighted, at the left or
// right edge of the primary screen. empty is shown when there are no items.
func (o *Overlay) Show(items []OverlayItem, sel int, left bool, empty string) {
	mon, scale := primaryMonitor()
	o.mon = mon
	o.scene = overlayScene{
		items: items, sel: clampSelection(sel, len(items)), left: left, scale: scale,
		maxH: int(float64(mon.h()) / scale * ovMaxShare), nameMax: ovNameMax, empty: empty,
	}
	now := time.Now()
	if o.phase == ovHidden || o.phase == ovHiding {
		o.phase, o.phaseStart = ovShowing, now
	}
	o.growStart = now
	o.flashAt, o.onHidden = time.Time{}, nil // opened again during a flash or slide-out
	o.render()
	o.present(o.slide(now))
	// Topmost again: a game may have put itself above since.
	procSetWindowPos.Call(o.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpShowWindow)
	o.startTimer()
}

// SetSelected moves the highlight; it grows anew at its new place.
func (o *Overlay) SetSelected(sel int) {
	sel = clampSelection(sel, len(o.scene.items))
	if sel == o.scene.sel || !o.Visible() {
		return
	}
	o.scene.sel, o.scene.flash = sel, 0
	o.growStart = time.Now()
	o.startTimer()
}

// Confirm flashes the highlight (green when the sound plays, red when it
// can't), then slides the overlay out.
func (o *Overlay) Confirm(ok bool) {
	if !o.Visible() {
		return
	}
	o.scene.flash, o.scene.grow = flashOK, 1
	if !ok {
		o.scene.flash = flashBad
	}
	o.flashAt = time.Now()
	o.render()
	o.present(o.slide(time.Now()))
	o.startTimer()
}

// Hide slides the overlay out; onHidden (if set) runs once it is gone.
func (o *Overlay) Hide(onHidden func()) {
	if o.phase == ovHidden {
		if onHidden != nil {
			onHidden()
		}
		return
	}
	if o.phase != ovHiding {
		o.phase, o.phaseStart = ovHiding, time.Now()
	}
	o.onHidden = onHidden
	o.flashAt = time.Time{}
	o.startTimer()
}

func (o *Overlay) startTimer() {
	if !o.timer {
		procSetTimer.Call(o.hwnd, overlayTimerID, overlayFrameMS, 0)
		o.timer = true
	}
}

func (o *Overlay) stopTimer() {
	if o.timer {
		procKillTimer.Call(o.hwnd, overlayTimerID)
		o.timer = false
	}
}

// slide returns the slide-in progress (0 hidden at the edge, 1 in place).
func (o *Overlay) slide(now time.Time) float64 {
	el := now.Sub(o.phaseStart)
	switch o.phase {
	case ovShowing:
		return easeOut(float64(el) / float64(overlaySlideIn))
	case ovHiding:
		return 1 - easeOut(float64(el)/float64(overlaySlideOut))
	case ovShown:
		return 1
	}
	return 0
}

// tick advances the animations; the timer runs only while one does.
func (o *Overlay) tick() {
	now := time.Now()
	p := o.slide(now)
	switch {
	case o.phase == ovShowing && p >= 1:
		o.phase = ovShown
	case o.phase == ovHiding && p <= 0:
		o.phase = ovHidden
		o.stopTimer()
		procShowWindow.Call(o.hwnd, swHide)
		o.scene.flash = 0
		if f := o.onHidden; f != nil {
			o.onHidden = nil
			f()
		}
		return
	}
	if !o.flashAt.IsZero() && now.Sub(o.flashAt) >= overlayFlash {
		o.flashAt = time.Time{}
		o.Hide(nil)
		p = o.slide(now)
	}
	grow := easeOut(float64(now.Sub(o.growStart)) / float64(overlayGrow))
	if o.scene.flash != 0 {
		grow = 1
	}
	if grow != o.scene.grow {
		o.scene.grow = grow
		o.render()
	}
	o.present(p)
	if o.phase == ovShown && grow >= 1 && o.flashAt.IsZero() {
		o.stopTimer()
	}
}

func (o *Overlay) render() {
	surf, err := renderOverlay(o.scene)
	if err != nil {
		return // keeps the last frame
	}
	o.surf.free()
	o.surf = surf
}

// present puts the current frame on screen, slid in by p (0..1) and as
// opaque as far as it slid.
func (o *Overlay) present(p float64) {
	if o.surf == nil {
		return
	}
	s := o.scene.scale
	edge := int32(float64(ovEdge-ovPad) * s) // the shadow's padding reaches past the margin
	travel := int32(float64(ovEdge+ovBar) * s * (1 - p))
	y := o.mon.Top + (o.mon.h()-o.surf.h)/2
	x := o.mon.Right - edge - o.surf.w + travel
	if o.scene.left {
		x = o.mon.Left + edge - travel
	}
	screen, _, _ := procGetDC.Call(0)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	old, _, _ := procSelectObject.Call(mem, o.surf.bmp)
	dst := point{x, y}
	size := struct{ CX, CY int32 }{o.surf.w, o.surf.h}
	var src point
	alpha := uint32(max(0, min(255, p*255)))
	blend := uint32(0 | 0<<8 | alpha<<16 | 1<<24) // AC_SRC_OVER, constant alpha, AC_SRC_ALPHA
	procUpdateLayeredWindow.Call(o.hwnd, screen, uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&size)),
		mem, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	procSelectObject.Call(mem, old)
	procDeleteDC.Call(mem)
	procReleaseDC.Call(0, screen)
	procShowWindow.Call(o.hwnd, swShowNoActivate)
}
