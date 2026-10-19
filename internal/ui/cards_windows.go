package ui

// PageCards: two large tiles side by side, each under its own section caption,
// as on the home page (the sound library and the web app). A card works
// like a Nav: a click opens its page. It has a title in the top left, a
// short status at the bottom with a chevron, an optional tool button in the
// top right (such as refresh) and a large, faint icon behind the text.

// PageCard is one tile of a PageCards row.
type PageCard struct {
	Section string // caption above the card, e.g. "Sound library"
	Title   string // top left, e.g. "Folders"
	Detail  string // bottom left status, e.g. "157 sounds · 4 tabs"
	Tone    Tone   // Detail's colour
	Art     Icon   // the faint icon in the background; "" for none
	OnClick func() // opens the card's page

	Tool    Icon   // the top right button's icon; "" for none
	ToolTip string // its tooltip
	OnTool  func() // nil shows the button disabled
}

type pageCardsRow struct{ cards [2]PageCard }

// PageCards places two cards next to each other, each half the panel's width.
func PageCards(left, right PageCard) Row { return pageCardsRow{[2]PageCard{left, right}} }

// Logical sizes. A card is about three Nav tiles tall.
const (
	pageCardCaptionL = 28 // like a Section row
	pageCardHeightL  = 106
	pageCardGapL     = 8  // between the two cards
	pageCardPadL     = 12 // inside a card, around the text
	pageCardToolL    = 30 // the tool button, as big as a header's back button
	pageCardArtL     = 92 // the background icon
)

// Segments are ordered tool first, so a click on the tool button doesn't
// also count as a click on the card around it.
const (
	pageCardSegTool = iota
	pageCardSegBox
	pageCardSegs // segments per card
)

func (pageCardsRow) height() int { return pageCardCaptionL + pageCardHeightL + 4 }

// column is card i's horizontal extent, caption and tile.
func (pageCardsRow) column(c *canvas, r rect, i int) (left, right int32) {
	in := inset(c, r)
	w := (in.w() - c.px(pageCardGapL)) / 2
	if i == 0 {
		return in.Left, in.Left + w
	}
	return in.Right - w, in.Right
}

func (cr pageCardsRow) box(c *canvas, r rect, i int) rect {
	left, right := cr.column(c, r, i)
	top := r.Top + c.px(pageCardCaptionL) + c.px(2)
	return rect{left, top, right, top + c.px(pageCardHeightL)}
}

// tool is card i's tool button in the top right corner of its tile.
func (cr pageCardsRow) tool(c *canvas, r rect, i int) rect {
	box := cr.box(c, r, i)
	pad, size := c.px(6), c.px(pageCardToolL)
	return rect{box.Right - pad - size, box.Top + pad, box.Right - pad, box.Top + pad + size}
}

func (cr pageCardsRow) segments(c *canvas, r rect) []rect {
	segs := make([]rect, 0, len(cr.cards)*pageCardSegs)
	for i, card := range cr.cards {
		var tool rect
		if card.Tool != "" && card.OnTool != nil {
			tool = cr.tool(c, r, i)
		}
		segs = append(segs, tool, cr.box(c, r, i))
	}
	return segs
}

func (cr pageCardsRow) click(seg int) {
	if seg < 0 || seg >= len(cr.cards)*pageCardSegs {
		return
	}
	card := cr.cards[seg/pageCardSegs]
	switch {
	case seg%pageCardSegs == pageCardSegTool && card.OnTool != nil:
		card.OnTool()
	case seg%pageCardSegs == pageCardSegBox && card.OnClick != nil:
		card.OnClick()
	}
}

func (pageCardsRow) keepsOpen() bool { return true }

// tip is the tool button's tooltip, or a card's full status when it is cut
// off.
func (cr pageCardsRow) tip(c *canvas, r rect, seg int) string {
	if seg < 0 || seg >= len(cr.cards)*pageCardSegs {
		return ""
	}
	i := seg / pageCardSegs
	card := cr.cards[i]
	if seg%pageCardSegs == pageCardSegTool {
		return card.ToolTip
	}
	if _, status := cr.textBoxes(c, r, i); c.textWidth(card.Detail, fontBody) > status.w() {
		return card.Detail
	}
	return ""
}

// textBoxes are where card i's title (left of the tool button) and status
// (left of the chevron) go.
func (cr pageCardsRow) textBoxes(c *canvas, r rect, i int) (title, status rect) {
	box, tool := cr.box(c, r, i), cr.tool(c, r, i)
	pad := c.px(pageCardPadL)
	right := box.Right - pad
	if cr.cards[i].Tool != "" {
		right = tool.Left - c.px(4)
	}
	title = rect{box.Left + pad, tool.Top, right, tool.Bottom}
	bottom := box.Bottom - c.px(8)
	// The chevron's glyph is narrow inside its box: no extra gap needed.
	status = rect{box.Left + pad, bottom - tool.h(), cr.chevron(c, r, i).Left, bottom}
	return title, status
}

func (cr pageCardsRow) chevron(c *canvas, r rect, i int) rect {
	box := cr.box(c, r, i)
	bottom := box.Bottom - c.px(8)
	right := box.Right - c.px(7) // the glyph ends about where the tool icon does
	return rect{right - c.px(16), bottom - c.px(pageCardToolL), right, bottom}
}

func (cr pageCardsRow) paint(c *canvas, r rect, st rowState) {
	for i := range cr.cards {
		cr.paintCard(c, r, i, st)
	}
}

func (cr pageCardsRow) paintCard(c *canvas, r rect, i int, st rowState) {
	card := cr.cards[i]
	toolSeg, boxSeg := i*pageCardSegs+pageCardSegTool, i*pageCardSegs+pageCardSegBox

	left, right := cr.column(c, r, i)
	c.text(upper(card.Section), rect{left, r.Top + c.px(8), right, r.Top + c.px(pageCardCaptionL)}, fontCaption, colCaption, dtLeft)

	box := cr.box(c, r, i)
	bg := colRowHover
	switch {
	case st.press == boxSeg:
		bg = colRowPress
	case st.hover == boxSeg:
		bg = shade(colRowHover, 10)
	}
	radius := c.px(12)
	c.roundRect(box, radius, bg)

	// The background icon, a shade darker than the tile, follows the tile's
	// hover colour. It reaches past the right edge, clipped to the tile.
	if card.Art != "" {
		size := c.px(pageCardArtL)
		art := rect{box.Right - size + c.px(14), box.Top + (box.h()-size)/2 + c.px(8), 0, 0}
		art.Right, art.Bottom = art.Left+size, art.Top+size
		restore := c.clipRound(box, radius)
		c.icon(card.Art, art, pageCardArtL, shade(bg, -9))
		restore()
	}

	if card.Tool != "" {
		tool := cr.tool(c, r, i)
		col := colText
		switch {
		case card.OnTool == nil:
			col = colMuted
		case st.press == toolSeg:
			c.roundRect(tool, c.px(8), shade(bg, 22))
		case st.hover == toolSeg:
			c.roundRect(tool, c.px(8), shade(bg, 12))
		}
		c.icon(card.Tool, tool, 18, col)
	}

	title, status := cr.textBoxes(c, r, i)
	c.text(card.Title, title, fontBold, colText, dtLeft)
	c.text(card.Detail, status, fontBody, card.Tone.color(), dtLeft)
	c.icon(IconChevronRight, cr.chevron(c, r, i), 16, colMuted)
}
