package ui

import "time"

// The launchpad's badge shows like the in-game overlay (layered_windows.go):
// over the game, never taking the focus, sliding in from its edge. Frames
// are drawn with Direct2D: a dark capsule with the yellow button at the end
// towards the screen's edge (left in the middle places), the text beside
// it, and the overlay's drop shadow beneath.

// badgeSoundFor is how long a played sound's name stays before the tab's
// comes back.
const badgeSoundFor = 1200 * time.Millisecond

var (
	// LaunchpadYellow is the launchpad's colour, also the phone's while it
	// is held (webapp styles.css, --launchpad-rgb).
	LaunchpadYellow = rgb(0xFF, 0xC9, 0x3C)
	bdDotsColor     = rgbaF(rgb(0x3A, 0x2E, 0x08), 1)
)

// badgeScene is what one frame shows.
type badgeScene struct {
	scale  float64
	right  bool   // at the screen's right edge: the text left of the button
	text   string // beside the button; "" with no emoji: the button alone
	emoji  string // before the text: a played sound's
	flash  int    // 0, flashOK or flashBad: a play confirmed
	shadow overlayShadow
}

// renderBadge draws a scene into a new surface.
func renderBadge(sc badgeScene) (*surface, error) {
	g, err := getD2D()
	if err != nil {
		return nil, err
	}
	s := float32(sc.scale)
	px := func(v int) float32 { return float32(v) * s }
	font := fontSpec{family: "Segoe UI", size: 16 * s, weight: 700}
	emojiFont := fontSpec{family: "Segoe UI Emoji", size: 19 * s, weight: 400}
	text := TruncateName(sc.text, bdChars)
	var textW, emojiW float32
	if text != "" {
		textW = min(g.measure(text, font), px(bdTextMax))
	}
	if sc.emoji != "" {
		emojiW = g.measure(sc.emoji, emojiFont)
		if text != "" {
			emojiW += px(bdEmojiGap)
		}
	}
	capW := px(bdHeight)
	if textW+emojiW > 0 {
		capW = px(bdHeight)/2 + px(bdDot)/2 + px(bdTextGap) + emojiW + textW + px(bdTextEnd)
	}
	pad := sc.shadow.pad()
	surf, err := newSurface(int32(capW+px(2*pad)+0.5), int32(px(bdHeight+2*pad)+0.5))
	if err != nil {
		return nil, err
	}
	capsule := rectF{px(pad), px(pad), px(pad) + capW, px(pad + bdHeight)}
	cy := (capsule.T + capsule.B) / 2
	cx := capsule.L + px(bdHeight)/2
	if sc.right {
		cx = capsule.R - px(bdHeight)/2
	}
	fill, fg := ovBarColor, ovTextColor
	switch sc.flash {
	case flashOK:
		fill, fg = ovFlashOK, ovFlashOKText
	case flashBad:
		fill = ovFlashBad
	}

	p, err := g.begin(surf)
	if err != nil {
		surf.free()
		return nil, err
	}
	p.fillRound(capsule, px(bdHeight)/2, fill)
	p.fillCircle(cx, cy, px(bdDot)/2, rgbaF(LaunchpadYellow, 1))
	// A launchpad's pads: three rows of three dots.
	step, dot := px(8), px(5)/2
	for row := -1; row <= 1; row++ {
		for col := -1; col <= 1; col++ {
			p.fillCircle(cx+float32(col)*step, cy+float32(row)*step, dot, bdDotsColor)
		}
	}
	if textW+emojiW > 0 {
		x := capsule.L + px(bdHeight)/2 + px(bdDot)/2 + px(bdTextGap)
		if sc.right {
			x = capsule.L + px(bdTextEnd)
		}
		if sc.emoji != "" {
			p.text(sc.emoji, rectF{x - 2, capsule.T, x + emojiW + 4, capsule.B}, emojiFont, fg)
			x += emojiW
		}
		if text != "" {
			p.text(text, rectF{x, capsule.T, x + textW + 2, capsule.B}, font, fg)
		}
	}
	if err := p.end(); err != nil {
		surf.free()
		return nil, err
	}
	shadow := newShadowLayer(surf)
	shadow.add(capsule, px(bdHeight)/2, s, sc.shadow)
	shadow.under(surf)
	return surf, nil
}

// LaunchpadBadge is the launchpad's badge. Its methods run on the UI thread.
type LaunchpadBadge struct {
	*layered
	scene      badgeScene
	at         BadgeAt
	label      string    // shown while no sound is
	flashAt    time.Time // when the confirming flash began
	soundUntil time.Time // a played sound shows until then; zero = none
}

// NewLaunchpadBadge creates the badge's window (hidden).
func NewLaunchpadBadge() (*LaunchpadBadge, error) {
	b := &LaunchpadBadge{}
	l, err := newLayered(func() { b.tick() })
	if err != nil {
		return nil, err
	}
	b.layered = l
	l.place = func(mon rect, w, h int32, p float64) (dst, src point, cw, ch int32) {
		s := b.scene.scale
		return badgePlacement(mon, w, h, b.at, int32(float64(b.scene.shadow.pad())*s+0.5), int32(float64(bdMargin)*s+0.5), p)
	}
	return b, nil
}

// Show slides the badge in at at, with label beside the button ("" for the
// button alone), or changes them while it shows. A sound played a moment
// ago keeps showing until its time is up.
func (b *LaunchpadBadge) Show(at BadgeAt, label string) {
	now := time.Now()
	moved := b.Visible() && at != b.at
	b.at, b.label = at, label
	scale := b.open(now)
	b.scene.scale, b.scene.right, b.scene.shadow = scale, at.right(), ovShadowGame
	if b.soundUntil.IsZero() {
		b.scene.text, b.scene.emoji = label, ""
	}
	b.render()
	if moved { // a new place: slide in there anew
		b.phase, b.phaseStart = ovShowing, now
	}
	b.present(b.slide(now))
	b.raise()
	b.startTimer()
}

// Played shows a sound just played from the launchpad: its emoji and name
// for a moment (with name "", only the flash), flashing green, or red when
// it couldn't play.
func (b *LaunchpadBadge) Played(emoji, name string, ok bool) {
	if !b.Visible() {
		return
	}
	now := time.Now()
	b.scene.flash = flashOK
	if !ok {
		b.scene.flash = flashBad
	}
	b.flashAt = now
	if name != "" {
		b.scene.text, b.scene.emoji = name, emoji
		b.soundUntil = now.Add(badgeSoundFor)
	}
	b.render()
	b.present(b.slide(now))
	b.startTimer()
}

// Hide slides the badge out.
func (b *LaunchpadBadge) Hide() { b.layered.Hide(nil) }

// tick advances the animations; the timer runs only while one does.
func (b *LaunchpadBadge) tick() {
	now := time.Now()
	p, gone := b.advance(now)
	if gone {
		b.flashAt, b.soundUntil = time.Time{}, time.Time{}
		b.scene.flash, b.scene.text, b.scene.emoji = 0, b.label, ""
		b.gone()
		return
	}
	redraw := false
	if !b.flashAt.IsZero() && now.Sub(b.flashAt) >= overlayFlash {
		b.flashAt, b.scene.flash, redraw = time.Time{}, 0, true
	}
	if !b.soundUntil.IsZero() && now.After(b.soundUntil) {
		b.soundUntil, b.scene.text, b.scene.emoji, redraw = time.Time{}, b.label, "", true
	}
	if redraw {
		b.render()
	}
	b.present(p)
	if b.phase == ovShown && b.flashAt.IsZero() && b.soundUntil.IsZero() {
		b.stopTimer()
	}
}

func (b *LaunchpadBadge) render() {
	surf, _ := renderBadge(b.scene) // on failure, keeps the last frame
	b.setFrame(surf)
}
