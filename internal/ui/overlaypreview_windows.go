package ui

import (
	"fmt"
	"strings"
)

// OverlayPreview is a small picture of the in-game overlay for the panel,
// drawn by the overlay's own renderer at a smaller size, with a paragraph
// beside it (like ImportGuide). items are the first few favorites (or
// examples), sel the highlighted one.
func OverlayPreview(text string, items []OverlayItem, sel int, left bool) Row {
	return overlayPreviewRow{text, items, sel, left}
}

type overlayPreviewRow struct {
	text  string
	items []OverlayItem
	sel   int
	left  bool
}

const (
	previewScale   = 0.55 // of the overlay's size
	previewNameMax = 110  // logical, at the overlay's size
	previewGapL    = 12
	previewItems   = 5
)

func (overlayPreviewRow) height() int                   { return 150 }
func (overlayPreviewRow) segments(*canvas, rect) []rect { return nil }
func (overlayPreviewRow) click(int)                     {}
func (overlayPreviewRow) keepsOpen() bool               { return true }

// previewCache keeps the last picture: rows are rebuilt on every render.
var previewCache struct {
	key  string
	surf *surface
}

func (o overlayPreviewRow) picture(c *canvas) *surface {
	items := o.items[:min(len(o.items), previewItems)]
	var key strings.Builder
	fmt.Fprintf(&key, "%v|%d|%v|%.3f", o.left, o.sel, len(items), c.scale)
	for _, it := range items {
		fmt.Fprintf(&key, "|%s\x00%s\x00%s", it.Name, it.Emoji, it.Color)
	}
	if previewCache.surf != nil && previewCache.key == key.String() {
		return previewCache.surf
	}
	surf, err := renderOverlay(overlayScene{
		items: items, sel: o.sel, left: o.left, scale: c.scale * previewScale,
		maxH: 10000, nameMax: previewNameMax, grow: 1, empty: "No favorites yet",
	})
	if err != nil {
		return nil
	}
	previewCache.surf.free()
	previewCache.key, previewCache.surf = key.String(), surf
	return surf
}

func (o overlayPreviewRow) measure(c *canvas, width int32) int32 {
	pic := o.picture(c)
	if pic == nil {
		return c.wrapHeight(o.text, width-2*c.px(padX), fontBody) + c.px(8)
	}
	textW := width - 2*c.px(padX) - pic.w - c.px(previewGapL)
	return max(pic.h, c.wrapHeight(o.text, textW, fontBody)+c.px(8))
}

func (o overlayPreviewRow) paint(c *canvas, r rect, _ rowState) {
	in := inset(c, r)
	pic := o.picture(c)
	textR := rect{in.Left, r.Top + c.px(4), in.Right, r.Bottom}
	if pic != nil {
		// The picture's shadow padding reaches into the panel's padding.
		x := in.Right - pic.w + int32(float64(ovPad)*previewScale*c.scale)
		c.blit(&dib{bmp: pic.bmp, w: pic.w, h: pic.h}, x, r.Top+(r.h()-pic.h)/2)
		textR.Right = x - c.px(previewGapL)
		textR.Top = r.Top + (r.h()-c.wrapHeight(o.text, textR.w(), fontBody))/2
	}
	c.textWrapped(o.text, textR, fontBody, colMuted)
}
