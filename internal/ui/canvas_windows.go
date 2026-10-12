package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Theme colors (COLORREF). The panel is dark-only for now.
var (
	colBackground = rgb(0x1C, 0x1D, 0x22)
	colRowHover   = rgb(0x2A, 0x2C, 0x33)
	colRowPress   = rgb(0x34, 0x37, 0x40)
	colText       = rgb(0xEC, 0xEC, 0xF0)
	colMuted      = rgb(0x9A, 0x9C, 0xA6)
	colCaption    = rgb(0x7E, 0x81, 0x8C)
	colDivider    = rgb(0x33, 0x35, 0x3C)
	colAccent     = rgb(0x50, 0xDE, 0xA8)
	colAccentText = rgb(0x10, 0x2A, 0x20)
	colDanger     = rgb(0xF2, 0x41, 0x6F) // berry red
	colWarning    = rgb(0xF2, 0xC1, 0x4E)
	colControl    = rgb(0x3A, 0x3D, 0x46)
	colList       = rgb(0x25, 0x27, 0x2D) // dropdown lists and tooltips
	colTip        = rgb(0x30, 0x32, 0x3A)
)

// fontKind selects one of the panel fonts.
type fontKind int

const (
	fontBody fontKind = iota
	fontBold
	fontCaption
	fontTitle
	fontCount
)

// fonts holds the GDI fonts for one DPI scale.
type fonts struct {
	scale float64
	h     [fontCount]uintptr
}

func newFonts(scale float64) *fonts {
	f := &fonts{scale: scale}
	spec := [fontCount]struct {
		size   float64
		weight int32
	}{
		fontBody:    {13, 400},
		fontBold:    {13, 600},
		fontCaption: {11, 600},
		fontTitle:   {16, 600},
	}
	for k, s := range spec {
		lf := logFont{Height: -int32(s.size*scale + 0.5), Weight: s.weight, Quality: 5 /* CLEARTYPE_QUALITY */}
		copy(lf.FaceName[:], []uint16{'S', 'e', 'g', 'o', 'e', ' ', 'U', 'I', 0})
		h, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
		f.h[k] = h
	}
	return f
}

func (f *fonts) destroy() {
	for _, h := range f.h {
		if h != 0 {
			procDeleteObject.Call(h)
		}
	}
}

// canvas wraps a device context with the current scale and fonts.
type canvas struct {
	dc    uintptr
	scale float64
	fonts *fonts
}

// px converts logical (96-DPI) pixels to device pixels.
func (c *canvas) px(v int) int32 { return int32(float64(v)*c.scale + 0.5) }

func (c *canvas) fill(r rect, color uint32) {
	b, _, _ := procCreateSolidBrush.Call(uintptr(color))
	procFillRect.Call(c.dc, uintptr(unsafe.Pointer(&r)), b)
	procDeleteObject.Call(b)
}

// roundRect fills a rounded rectangle without an outline.
func (c *canvas) roundRect(r rect, radius int32, color uint32) {
	b, _, _ := procCreateSolidBrush.Call(uintptr(color))
	pen, _, _ := procGetStockObject.Call(nullPen)
	oldB, _, _ := procSelectObject.Call(c.dc, b)
	oldP, _, _ := procSelectObject.Call(c.dc, pen)
	// RoundRect excludes the right/bottom edge; +1 keeps the full size.
	procRoundRect.Call(c.dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right+1), uintptr(r.Bottom+1), uintptr(radius), uintptr(radius))
	procSelectObject.Call(c.dc, oldB)
	procSelectObject.Call(c.dc, oldP)
	procDeleteObject.Call(b)
}

// ring draws a circle outline of the given width, optionally with a filled dot.
func (c *canvas) circle(cx, cy, radius int32, color uint32) {
	b, _, _ := procCreateSolidBrush.Call(uintptr(color))
	pen, _, _ := procGetStockObject.Call(nullPen)
	oldB, _, _ := procSelectObject.Call(c.dc, b)
	oldP, _, _ := procSelectObject.Call(c.dc, pen)
	procEllipse.Call(c.dc, uintptr(cx-radius), uintptr(cy-radius), uintptr(cx+radius+1), uintptr(cy+radius+1))
	procSelectObject.Call(c.dc, oldB)
	procSelectObject.Call(c.dc, oldP)
	procDeleteObject.Call(b)
}

// text draws single-line text with ellipsis inside r.
func (c *canvas) text(s string, r rect, font fontKind, color uint32, align uint32) {
	old, _, _ := procSelectObject.Call(c.dc, c.fonts.h[font])
	procSetTextColor.Call(c.dc, uintptr(color))
	procSetBkMode.Call(c.dc, transparent)
	u := utf16Ptr(s)
	procDrawTextW.Call(c.dc, uintptr(unsafe.Pointer(u)), ^uintptr(0), uintptr(unsafe.Pointer(&r)),
		uintptr(align|dtSingleLine|dtVCenter|dtNoPrefix|dtEndEllipsis))
	procSelectObject.Call(c.dc, old)
}

var (
	procGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	procMoveToEx              = gdi32.NewProc("MoveToEx")
	procLineTo                = gdi32.NewProc("LineTo")
	procGetDC                 = user32.NewProc("GetDC")
	procReleaseDC             = user32.NewProc("ReleaseDC")
	procSetTimer              = user32.NewProc("SetTimer")
	procKillTimer             = user32.NewProc("KillTimer")
)

const (
	dtWordBreak = 0x0010
	dtCalcRect  = 0x0400
)

// textWidth measures single-line text in device pixels.
func (c *canvas) textWidth(s string, font fontKind) int32 {
	u, err := utf16Slice(s)
	if err != nil || len(u) == 0 || c.dc == 0 {
		return 0
	}
	old, _, _ := procSelectObject.Call(c.dc, c.fonts.h[font])
	var size struct{ CX, CY int32 }
	procGetTextExtentPoint32W.Call(c.dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(unsafe.Pointer(&size)))
	procSelectObject.Call(c.dc, old)
	return size.CX
}

// wrapHeight returns the height of s wrapped to width.
func (c *canvas) wrapHeight(s string, width int32, font fontKind) int32 {
	if c.dc == 0 {
		return 0
	}
	old, _, _ := procSelectObject.Call(c.dc, c.fonts.h[font])
	r := rect{0, 0, width, 0}
	procDrawTextW.Call(c.dc, uintptr(unsafe.Pointer(utf16Ptr(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)),
		dtWordBreak|dtCalcRect|dtNoPrefix)
	procSelectObject.Call(c.dc, old)
	return r.Bottom
}

// textWrapped draws multi-line, word-wrapped text inside r.
func (c *canvas) textWrapped(s string, r rect, font fontKind, color uint32) {
	old, _, _ := procSelectObject.Call(c.dc, c.fonts.h[font])
	procSetTextColor.Call(c.dc, uintptr(color))
	procSetBkMode.Call(c.dc, transparent)
	procDrawTextW.Call(c.dc, uintptr(unsafe.Pointer(utf16Ptr(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)),
		dtWordBreak|dtNoPrefix)
	procSelectObject.Call(c.dc, old)
}

// frame fills a rounded rectangle with a 1-pixel border.
func (c *canvas) frame(r rect, radius int32, fill, border uint32) {
	b, _, _ := procCreateSolidBrush.Call(uintptr(fill))
	pen, _, _ := procCreatePen.Call(0 /* PS_SOLID */, uintptr(max(1, c.px(1))), uintptr(border))
	oldB, _, _ := procSelectObject.Call(c.dc, b)
	oldP, _, _ := procSelectObject.Call(c.dc, pen)
	procRoundRect.Call(c.dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius), uintptr(radius))
	procSelectObject.Call(c.dc, oldB)
	procSelectObject.Call(c.dc, oldP)
	procDeleteObject.Call(b)
	procDeleteObject.Call(pen)
}

// chevron draws a "v" (or "^" when up) centred on (cx, cy).
func (c *canvas) chevron(cx, cy, half int32, up bool, color uint32) {
	pen, _, _ := procCreatePen.Call(0, uintptr(max(1, c.px(2))), uintptr(color))
	old, _, _ := procSelectObject.Call(c.dc, pen)
	rise := half / 2
	if up {
		rise = -rise
	}
	procMoveToEx.Call(c.dc, uintptr(cx-half), uintptr(cy-rise), 0)
	procLineTo.Call(c.dc, uintptr(cx), uintptr(cy+rise))
	procLineTo.Call(c.dc, uintptr(cx+half), uintptr(cy-rise))
	procSelectObject.Call(c.dc, old)
	procDeleteObject.Call(pen)
}

func utf16Slice(s string) ([]uint16, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return nil, err
	}
	return u[:len(u)-1], nil // without the terminating 0
}

// dashedFrame draws a dashed, anti-aliased outline of a rounded rectangle,
// corners included (see dashedRoundRect). Cached per size and colour.
func (c *canvas) dashedFrame(r rect, radius int32, color uint32) {
	key := dashKey{r.w(), r.h(), radius, color}
	d, ok := dashCache[key]
	if !ok {
		w := float64(max(1, c.px(1)))
		if c.scale >= 1.75 {
			w = float64(c.px(1)) + 0.5 // a touch bolder on high-DPI screens
		}
		mask := dashedRoundRect(int(r.w()), int(r.h()), float64(radius), w, float64(c.px(6)), float64(c.px(4)))
		d = newDIB(tintMask(mask, color), r.w(), r.h())
		dashCache[key] = d
	}
	c.blit(d, r.Left, r.Top)
}

type dashKey struct {
	w, h, radius int32
	color        uint32
}

var dashCache = map[dashKey]*dib{} // UI thread only
