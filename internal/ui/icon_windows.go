package ui

import (
	"image"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dib is a 32-bit top-down DIB section with premultiplied alpha, ready for
// AlphaBlend. Created on demand and cached for the life of the process
// (a handful of icons per DPI scale).
type dib struct {
	bmp  uintptr
	w, h int32
}

var (
	msimg32              = windows.NewLazySystemDLL("msimg32.dll")
	procAlphaBlend       = msimg32.NewProc("AlphaBlend")
	procCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	procSetTextCharExtra = gdi32.NewProc("SetTextCharacterExtra")
)

type iconKey struct {
	name  Icon
	size  int32
	color uint32
}

type imageKey struct {
	img  *Image
	size int32
}

// Caches are only touched on the UI thread. A nil entry remembers a failure.
var (
	iconCache  = map[iconKey]*dib{}
	imageCache = map[imageKey]*dib{}
)

// newDIB copies premultiplied RGBA pixels into a DIB section (BGRA).
func newDIB(pix []uint8, w, h int32) *dib {
	type bitmapInfoHeader struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
	}
	hdr := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32} // negative: top-down
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&hdr)), 0 /* DIB_RGB_COLORS */, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil
	}
	dst := unsafe.Slice((*uint8)(bits), len(pix))
	for i := 0; i+3 < len(pix); i += 4 {
		dst[i], dst[i+1], dst[i+2], dst[i+3] = pix[i+2], pix[i+1], pix[i], pix[i+3]
	}
	return &dib{bmp: bmp, w: w, h: h}
}

// blit alpha-blends d with its top-left corner at (x, y).
func (c *canvas) blit(d *dib, x, y int32) {
	if d == nil || c.dc == 0 {
		return
	}
	src, _, _ := procCreateCompatibleDC.Call(c.dc)
	old, _, _ := procSelectObject.Call(src, d.bmp)
	const blend = 0 | 0<<8 | 255<<16 | 1<<24 // AC_SRC_OVER, constant alpha 255, AC_SRC_ALPHA
	procAlphaBlend.Call(c.dc, uintptr(x), uintptr(y), uintptr(d.w), uintptr(d.h),
		src, 0, 0, uintptr(d.w), uintptr(d.h), blend)
	procSelectObject.Call(src, old)
	procDeleteDC.Call(src)
}

type glowKey struct {
	spec  glowSpec
	scale int32 // one logical pixel's size, for the cache only
}

// glowCache holds the rendered glows per DPI scale (UI thread only).
var glowCache = map[glowKey]*dib{}

// glows draws the background glows of a panel of w×h pixels (see
// glow.go): the edge glow at the right edge a fixed distance above the
// bottom, and the corner glow in the top left corner. Both ignore scrolling.
func (c *canvas) glows(w, h int32) {
	// Right edge: the box holds the left half of the light.
	rx, ry := c.px(edgeGlow.radiusXL), c.px(edgeGlow.radiusYL)
	c.glow(edgeGlow, rx, 2*ry, float64(rx), float64(ry), w-rx, h-c.px(glowBottomL)-ry)
	// Top left corner: the box holds the light's bottom right quarter.
	rx, ry = c.px(cornerGlow.radiusXL), c.px(cornerGlow.radiusYL)
	c.glow(cornerGlow, rx, ry, 0, 0, 0, 0)
}

// glow draws a bw×bh box of g's light centred at (cx, cy) inside the box,
// with the box's top left corner at (x, y).
func (c *canvas) glow(g glowSpec, bw, bh int32, cx, cy float64, x, y int32) {
	key := glowKey{g, c.px(100)}
	d, ok := glowCache[key]
	if !ok {
		rx, ry := float64(c.px(g.radiusXL)), float64(c.px(g.radiusYL))
		d = newDIB(glowPixels(int(bw), int(bh), cx, cy, rx, ry, colAccent, g), bw, bh)
		glowCache[key] = d
	}
	c.blit(d, x, y)
}

// icon draws an embedded icon of logical size sizeL, centred in box, in
// color. Unknown icons draw nothing (go test checks all of them render).
func (c *canvas) icon(name Icon, box rect, sizeL int, color uint32) {
	if name == "" {
		return
	}
	size := c.px(sizeL)
	key := iconKey{name, size, color}
	d, ok := iconCache[key]
	if !ok {
		if mask, err := rasterIcon(name, int(size)); err == nil {
			d = newDIB(tintMask(mask, color), size, size)
		}
		iconCache[key] = d
	}
	c.blit(d, box.Left+(box.w()-size)/2, box.Top+(box.h()-size)/2)
}

// tintMask turns an alpha mask into premultiplied RGBA of one COLORREF.
func tintMask(mask *image.Alpha, color uint32) []uint8 {
	r, g, b := uint32(color&0xFF), uint32(color>>8&0xFF), uint32(color>>16&0xFF)
	pix := make([]uint8, 4*len(mask.Pix))
	for i, a := range mask.Pix {
		a := uint32(a)
		pix[4*i], pix[4*i+1], pix[4*i+2], pix[4*i+3] = uint8(r*a/255), uint8(g*a/255), uint8(b*a/255), uint8(a)
	}
	return pix
}

// image draws img scaled to fit a square of logical size sizeL at (x, y).
func (c *canvas) image(img *Image, x, y int32, sizeL int) {
	if img == nil {
		return
	}
	size := c.px(sizeL)
	key := imageKey{img, size}
	d, ok := imageCache[key]
	if !ok {
		d = newDIB(img.at(int(size)).Pix, size, size)
		imageCache[key] = d
	}
	c.blit(d, x, y)
}
