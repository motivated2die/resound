package ui

// Offer: a framed card that stands out from the rows around it, for one
// optional thing worth doing first, such as the guided setup's import from
// Soundux. An accent outline holds a title, a short text and a button of
// its own with an icon, tinted in the accent so it reads apart from both a
// primary and a normal button.

// Logical sizes.
const (
	offerPadL     = 12 // inside the frame
	offerMarginL  = 4  // above and below the frame
	offerTitleL   = 22 // the title line
	offerGapL     = 6  // between the title, the text and the button
	offerButtonL  = 34
	offerRadiusL  = 12
	offerFallback = 40 // the text's height without a device context to measure with
)

type offerRow struct {
	icon               Icon
	title, text, label string
	onClick            func()
}

// Offer is a card with a title, a word-wrapped text, and a button with
// icon and label that runs onClick. The row grows to fit the text.
func Offer(icon Icon, title, text, label string, onClick func()) Row {
	return offerRow{icon, title, text, label, onClick}
}

func (offerRow) height() int {
	return 2*offerMarginL + 2*offerPadL + offerTitleL + 2*offerGapL + offerFallback + offerButtonL
}

func (o offerRow) frameBox(c *canvas, r rect) rect {
	in := inset(c, r)
	return rect{in.Left, r.Top + c.px(offerMarginL), in.Right, r.Bottom - c.px(offerMarginL)}
}

// textWidth is the room for the text inside the frame.
func (o offerRow) textWidth(c *canvas, width int32) int32 {
	return width - 2*c.px(padX) - 2*c.px(offerPadL)
}

func (o offerRow) measure(c *canvas, width int32) int32 {
	h := c.wrapHeight(o.text, o.textWidth(c, width), fontBody)
	if h == 0 {
		return c.px(o.height())
	}
	return c.px(2*offerMarginL+2*offerPadL+offerTitleL+2*offerGapL+offerButtonL) + h
}

func (o offerRow) buttonBox(c *canvas, r rect) rect {
	f := o.frameBox(c, r)
	pad := c.px(offerPadL)
	return rect{f.Left + pad, f.Bottom - pad - c.px(offerButtonL), f.Right - pad, f.Bottom - pad}
}

func (o offerRow) segments(c *canvas, r rect) []rect { return []rect{o.buttonBox(c, r)} }
func (o offerRow) click(int) {
	if o.onClick != nil {
		o.onClick()
	}
}
func (offerRow) keepsOpen() bool { return true }

func (o offerRow) paint(c *canvas, r rect, st rowState) {
	f := o.frameBox(c, r)
	c.frame(f, c.px(offerRadiusL), mix(colBackground, colAccent, 0.06), mix(colBackground, colAccent, 0.55))
	pad := c.px(offerPadL)
	in := rect{f.Left + pad, f.Top + pad, f.Right - pad, f.Bottom - pad}

	title := rect{in.Left, in.Top, in.Right, in.Top + c.px(offerTitleL)}
	c.text(o.title, title, fontBold, colText, dtLeft)

	btn := o.buttonBox(c, r)
	text := rect{in.Left, title.Bottom + c.px(offerGapL), in.Right, btn.Top - c.px(offerGapL)}
	c.textWrapped(o.text, text, fontBody, colMuted)

	bg := mix(colBackground, colAccent, 0.18)
	switch {
	case st.press == 0:
		bg = mix(colBackground, colAccent, 0.12)
	case st.hover == 0:
		bg = mix(colBackground, colAccent, 0.26)
	}
	c.roundRect(btn, c.px(10), bg)
	paintButtonFace(c, btn, o.label, o.icon, colAccent)
}
