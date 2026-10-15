package ui

// OverlayPreview is a small, fixed picture of the in-game overlay for the
// panel, drawn by the overlay's own renderer at a smaller size: example
// buttons in color with emoji, one of them highlighted. Beside it, in a
// column of their own, text and a note below it. left mirrors the picture
// like the overlay at the left screen edge.
func OverlayPreview(text, note string, left bool) Row {
	return overlayPreviewRow{text, note, left}
}

type overlayPreviewRow struct {
	text, note string
	left       bool
}

const (
	previewScale   = 0.55 // of the overlay's size
	previewNameMax = 110  // logical, at the overlay's size
	previewGapL    = 14   // between the text and the picture
	previewParaL   = 8    // between the text and the note
)

// previewItems are the example favorites the picture shows.
var previewItems = []OverlayItem{
	{Name: "Airhorn", Emoji: "📯", Color: "red"},
	{Name: "Drumroll", Emoji: "🥁", Color: "orange"},
	{Name: "Bruh", Emoji: "😑", Color: "purple"},
	{Name: "Applause", Emoji: "👏", Color: "green"},
	{Name: "Sad trombone", Emoji: "🎺", Color: "blue"},
}

func (overlayPreviewRow) height() int                   { return 150 }
func (overlayPreviewRow) segments(*canvas, rect) []rect { return nil }
func (overlayPreviewRow) click(int)                     {}
func (overlayPreviewRow) keepsOpen() bool               { return true }

// previewCache keeps the picture: rows are rebuilt on every render.
var previewCache struct {
	left  bool
	scale float64
	surf  *surface
}

func (o overlayPreviewRow) picture(c *canvas) *surface {
	if previewCache.surf != nil && previewCache.left == o.left && previewCache.scale == c.scale {
		return previewCache.surf
	}
	surf, err := renderOverlay(overlayScene{
		items: previewItems, sel: 2, left: o.left, scale: c.scale * previewScale,
		maxH: 10000, nameMax: previewNameMax, shadow: ovShadowPanel,
	})
	if err != nil {
		return nil
	}
	previewCache.surf.free()
	previewCache.left, previewCache.scale, previewCache.surf = o.left, c.scale, surf
	return surf
}

// layout returns where the picture goes (x within the row) and the text
// column's width; the picture's padding on the screen edge's side reaches
// into the panel's padding.
func (o overlayPreviewRow) layout(c *canvas, in rect, pic *surface) (picX, textW int32) {
	if pic == nil {
		return in.Right, in.w()
	}
	picX = in.Right - pic.w + int32(float64(ovEdge)*previewScale*c.scale)
	return picX, picX - c.px(previewGapL) - in.Left
}

func (o overlayPreviewRow) textHeight(c *canvas, w int32) int32 {
	return c.wrapHeight(o.text, w, fontBody) + c.px(previewParaL) + c.wrapHeight(o.note, w, fontBody)
}

func (o overlayPreviewRow) measure(c *canvas, width int32) int32 {
	pic := o.picture(c)
	in := rect{c.px(padX), 0, width - c.px(padX), 0}
	_, textW := o.layout(c, in, pic)
	h := o.textHeight(c, textW) + c.px(8)
	if pic != nil {
		h = max(h, pic.h)
	}
	return h
}

func (o overlayPreviewRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	pic := o.picture(c)
	picX, textW := o.layout(c, in, pic)
	if pic != nil {
		c.blit(&dib{bmp: pic.bmp, w: pic.w, h: pic.h}, picX, r.Top+(r.h()-pic.h)/2)
	}
	// The text and the note, centred together beside the picture.
	textH := c.wrapHeight(o.text, textW, fontBody)
	top := r.Top + (r.h()-o.textHeight(c, textW))/2
	c.textWrapped(o.text, rect{in.Left, top, in.Left + textW, top + textH}, fontBody, colText)
	top += textH + c.px(previewParaL)
	c.textWrapped(o.note, rect{in.Left, top, in.Left + textW, r.Bottom}, fontBody, colMuted)
}
