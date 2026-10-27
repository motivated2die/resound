package ui

// Choices: two large cards side by side, one of them chosen, for a choice
// that changes the rows below, such as how the Soundpad import carries
// sounds over. Each card has an icon, a title, a radio mark and a short
// word-wrapped text; the chosen one is outlined and tinted in the accent. A
// click on a card chooses it.

// Choice is one card of a Choices row.
type Choice struct {
	Icon  Icon
	Title string
	Text  string
}

// Logical sizes.
const (
	choicePadL     = 12 // inside a card
	choiceMarginL  = 4  // above and below the cards
	choiceTitleL   = 22 // the icon and title line
	choiceTextGapL = 6  // between the title and the text
	choiceGapL     = 8  // between the two cards
	choiceRadiusL  = 12
	choiceFallback = 60 // the text's height without a device context to measure with
)

type choicesRow struct {
	cards    [2]Choice
	selected int
	onSelect func(int)
}

// Choices places two cards next to each other, each half the panel's width;
// selected is the chosen one (0 or 1) and onSelect gets the clicked one.
// The row grows to fit the longer text.
func Choices(left, right Choice, selected int, onSelect func(int)) Row {
	return choicesRow{[2]Choice{left, right}, selected, onSelect}
}

func (choicesRow) height() int {
	return 2*choiceMarginL + 2*choicePadL + choiceTitleL + choiceTextGapL + choiceFallback
}

// cardWidth is one card's width in a row width wide.
func (choicesRow) cardWidth(c *canvas, width int32) int32 {
	return (width - 2*c.px(padX) - c.px(choiceGapL)) / 2
}

func (cr choicesRow) measure(c *canvas, width int32) int32 {
	textW := cr.cardWidth(c, width) - 2*c.px(choicePadL)
	var h int32
	for _, card := range cr.cards {
		h = max(h, c.wrapHeight(card.Text, textW, fontBody))
	}
	if h == 0 {
		return c.px(cr.height())
	}
	return c.px(2*choiceMarginL+2*choicePadL+choiceTitleL+choiceTextGapL) + h
}

func (cr choicesRow) box(c *canvas, r rect, i int) rect {
	in := inset(c, r)
	w := cr.cardWidth(c, r.w())
	left := in.Left
	if i == 1 {
		left = in.Right - w
	}
	return rect{left, r.Top + c.px(choiceMarginL), left + w, r.Bottom - c.px(choiceMarginL)}
}

func (cr choicesRow) segments(c *canvas, r rect) []rect {
	return []rect{cr.box(c, r, 0), cr.box(c, r, 1)}
}

func (cr choicesRow) click(seg int) {
	if seg >= 0 && seg < len(cr.cards) && cr.onSelect != nil {
		cr.onSelect(seg)
	}
}

func (choicesRow) keepsOpen() bool { return true }

func (cr choicesRow) paint(c *canvas, r rect, st rowState) {
	for i, card := range cr.cards {
		box, radius := cr.box(c, r, i), c.px(choiceRadiusL)
		chosen := i == cr.selected
		fill, border := colRowHover, colRowHover
		switch {
		case chosen:
			fill, border = mix(colBackground, colAccent, 0.10), colAccent
		case st.press == i:
			fill, border = colRowPress, colRowPress
		case st.hover == i:
			fill, border = shade(colRowHover, 10), shade(colRowHover, 10)
		}
		c.frame(box, radius, fill, border)

		pad := c.px(choicePadL)
		in := rect{box.Left + pad, box.Top + pad, box.Right - pad, box.Bottom - pad}
		line := rect{in.Left, in.Top, in.Right, in.Top + c.px(choiceTitleL)}

		iconCol, ring := colMuted, colMuted
		if chosen {
			iconCol, ring = colAccent, colAccent
		}
		size := c.px(18)
		c.icon(card.Icon, rect{line.Left, line.Top + (line.h()-size)/2, line.Left + size, line.Top + (line.h()+size)/2}, 18, iconCol)

		cx, cy := line.Right-c.px(7), line.Top+line.h()/2
		c.circle(cx, cy, c.px(7), ring)
		c.circle(cx, cy, c.px(5), fill)
		if chosen {
			c.circle(cx, cy, c.px(3), colAccent)
		}

		title := rect{line.Left + size + c.px(8), line.Top, cx - c.px(12), line.Bottom}
		c.text(card.Title, title, fontBold, colText, dtLeft)
		text := rect{in.Left, line.Bottom + c.px(choiceTextGapL), in.Right, in.Bottom}
		c.textWrapped(card.Text, text, fontBody, colMuted)
	}
}
