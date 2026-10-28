package ui

// OverlayPreview is a small, fixed picture of the in-game overlay for the
// panel, drawn by the overlay's own renderer at a smaller size: example
// buttons in color with emoji, the highlighted one saying SCROLL between
// arrows. Beside it, in a column of their own from the row's top,
// paragraphs: the first in the text color with the first mention of
// underline underlined (the key; "" = none), the others muted. left mirrors
// the picture like the overlay at the left screen edge.
func OverlayPreview(left bool, underline string, paragraphs ...string) Row {
	return previewRow{paragraphs, underline, func(c *canvas) (*surface, int32) {
		// The bar's padding on the screen edge's side reaches into the panel's.
		return overlayPreviewPicture(c, left), int32(float64(ovEdge) * previewScale * c.scale)
	}}
}

// previewRow is a small picture on the right (OverlayPreview,
// LaunchpadPreview) with paragraphs beside it. picture returns it and how
// far its transparent padding may reach into the panel's on the right.
type previewRow struct {
	paragraphs []string
	underline  string
	picture    func(c *canvas) (pic *surface, reach int32)
}

const (
	previewScale   = 0.55 // of the overlay's size
	previewNameMax = 110  // logical, at the overlay's size
	previewGapL    = 14   // between the text and the picture
	previewParaL   = 8    // between paragraphs
	previewTopL    = 4    // above the first paragraph
)

// previewItems are the example favorites the picture shows.
var previewItems = []OverlayItem{
	{Name: "Airhorn", Emoji: "📯", Color: "red"},
	{Name: "Drumroll", Emoji: "🥁", Color: "orange"},
	{Name: "Bruh", Emoji: "😑", Color: "purple"},
	{Name: "Applause", Emoji: "👏", Color: "green"},
	{Name: "Sad trombone", Emoji: "🎺", Color: "blue"},
	{Name: "Vine boom", Emoji: "💥", Color: "pink"}, // one more below: the picture looks better uneven
}

func (previewRow) height() int                   { return 150 }
func (previewRow) segments(*canvas, rect) []rect { return nil }
func (previewRow) click(int)                     {}
func (previewRow) keepsOpen() bool               { return true }

// previewCache keeps the picture: rows are rebuilt on every render.
var previewCache struct {
	left  bool
	scale float64
	surf  *surface
}

func overlayPreviewPicture(c *canvas, left bool) *surface {
	if previewCache.surf != nil && previewCache.left == left && previewCache.scale == c.scale {
		return previewCache.surf
	}
	surf, err := renderOverlay(overlayScene{
		items: previewItems, sel: 2, left: left, scale: c.scale * previewScale,
		maxH: 10000, nameMax: previewNameMax, shadow: ovShadowPanel, hint: "SCROLL",
	})
	if err != nil {
		return nil
	}
	previewCache.surf.free()
	previewCache.left, previewCache.scale, previewCache.surf = left, c.scale, surf
	return surf
}

// layout returns where the picture goes (x within the row) and the text
// column's width.
func (o previewRow) layout(c *canvas, in rect, pic *surface, reach int32) (picX, textW int32) {
	if pic == nil {
		return in.Right, in.w()
	}
	picX = in.Right - pic.w + reach
	return picX, picX - c.px(previewGapL) - in.Left
}

// paragraphHeight is a paragraph's height; the first one is laid out word
// by word for its underline.
func (o previewRow) paragraphHeight(c *canvas, i int, w int32) int32 {
	if i == 0 && o.underline != "" {
		_, h := importGuideRow{o.paragraphs[0], o.underline}.layout(c, w)
		return h
	}
	return c.wrapHeight(o.paragraphs[i], w, fontBody)
}

func (o previewRow) textHeight(c *canvas, w int32) int32 {
	h := c.px(previewTopL)
	for i := range o.paragraphs {
		if i > 0 {
			h += c.px(previewParaL)
		}
		h += o.paragraphHeight(c, i, w)
	}
	return h
}

func (o previewRow) measure(c *canvas, width int32) int32 {
	pic, reach := o.picture(c)
	in := rect{c.px(padX), 0, width - c.px(padX), 0}
	_, textW := o.layout(c, in, pic, reach)
	h := o.textHeight(c, textW) + c.px(8)
	if pic != nil {
		h = max(h, pic.h)
	}
	return h
}

func (o previewRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	pic, reach := o.picture(c)
	picX, textW := o.layout(c, in, pic, reach)
	if pic != nil {
		c.blit(&dib{bmp: pic.bmp, w: pic.w, h: pic.h}, picX, r.Top+(r.h()-pic.h)/2)
	}
	// The paragraphs from the top, like text right under a page's header.
	top := r.Top + c.px(previewTopL)
	for i, p := range o.paragraphs {
		h := o.paragraphHeight(c, i, textW)
		switch {
		case i == 0 && o.underline != "":
			paintUnderlined(c, importGuideRow{p, o.underline}, in.Left, top, textW)
		case i == 0:
			c.textWrapped(p, rect{in.Left, top, in.Left + textW, top + h}, fontBody, colText)
		default:
			c.textWrapped(p, rect{in.Left, top, in.Left + textW, top + h}, fontBody, colMuted)
		}
		top += h + c.px(previewParaL)
	}
}

// paintUnderlined draws text with its underlined phrase (as laid out for
// the import guide) in the text color, the phrase underlined in the accent.
func paintUnderlined(c *canvas, g importGuideRow, left, top, width int32) {
	words, _ := g.layout(c, width)
	lineH := c.wrapHeight("Ag", c.px(200), fontBody)
	for _, w := range words {
		box := rect{left + w.x, top + w.y, left + w.x + w.w + c.px(2), top + w.y + lineH}
		c.text(w.text, box, fontBody, colText, dtLeft)
		if w.under {
			uw := w.w
			if w.joined {
				uw += w.space
			}
			y := box.Bottom - c.px(2)
			c.fill(rect{box.Left, y, box.Left + uw, y + max(1, c.px(1))}, colAccent)
		}
	}
}
