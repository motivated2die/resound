package ui

import (
	"errors"
	"fmt"
	"math"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Direct2D and DirectWrite draw the in-game overlay: anti-aliased shapes,
// text on a transparent background and color emoji, none of which GDI can.
// They are COM interfaces, called here through their vtables (no cgo). Only
// what the overlay needs is wrapped; everything runs on the UI thread.
//
// Drawing goes to a surface, a top-down 32-bit DIB with premultiplied alpha,
// as UpdateLayeredWindow and AlphaBlend take it. A DC render target draws
// into it in pixels (96 DPI: one DIP is one pixel). It renders in software:
// a hardware target copies every frame back from the GPU, which made the
// overlay's frames almost twice as slow (4.8 ms instead of 2.7 at 150%).

var (
	d2d1                   = windows.NewLazySystemDLL("d2d1.dll")
	dwriteDLL              = windows.NewLazySystemDLL("dwrite.dll")
	procD2D1CreateFactory  = d2d1.NewProc("D2D1CreateFactory")
	procDWriteCreateFactor = dwriteDLL.NewProc("DWriteCreateFactory")

	iidID2D1Factory   = windows.GUID{Data1: 0x06152247, Data2: 0x6f50, Data3: 0x465a, Data4: [8]byte{0x92, 0x45, 0x11, 0x8b, 0xfd, 0x3b, 0x60, 0x07}}
	iidIDWriteFactory = windows.GUID{Data1: 0xb859ee5a, Data2: 0xd838, Data3: 0x4b5b, Data4: [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}
)

// comObj is a COM interface pointer: its first word points to the vtable.
type comObj struct{ vtbl *[96]uintptr }

// call invokes vtable method i with the object as the first argument.
// Floats are only passed past the fourth argument, where the x64 calling
// convention puts them on the stack like integers.
//
// Arguments are often addresses of locals (out parameters, structs). Like
// LazyProc.Call, call is marked uintptrescapes, so those locals are moved to
// the heap and stay put during the call; on the stack they could move when
// it grows, and Windows would write to the old place.
//
//go:uintptrescapes
func (o *comObj) call(i int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[i], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comObj) release() {
	if o != nil {
		o.call(2)
	}
}

func hresult(r uintptr, what string) error {
	if int32(r) < 0 {
		return fmt.Errorf("%s: HRESULT %#x", what, uint32(r))
	}
	return nil
}

// Vtable indices (IUnknown takes 0-2).
const (
	d2dFactoryCreateDCRenderTarget = 16

	rtCreateSolidColorBrush = 8
	rtFillRectangle         = 17
	rtFillRoundedRectangle  = 19
	rtFillEllipse           = 21
	rtDrawText              = 27
	rtSetTextAntialiasMode  = 34
	rtClear                 = 47
	rtBeginDraw             = 48
	rtEndDraw               = 49
	dcrtBindDC              = 57

	brushSetColor = 8

	dwCreateTextFormat           = 15
	dwCreateTextLayout           = 18
	dwCreateEllipsisTrimmingSign = 20

	tfSetTextAlignment      = 3
	tfSetParagraphAlignment = 4
	tfSetWordWrapping       = 5
	tfSetTrimming           = 9
	tlGetMetrics            = 60
)

// colorF is D2D1_COLOR_F (straight alpha).
type colorF struct{ R, G, B, A float32 }

func rgbaF(c uint32, a float32) colorF {
	return colorF{float32(c&0xFF) / 255, float32(c>>8&0xFF) / 255, float32(c>>16&0xFF) / 255, a}
}

type rectF struct{ L, T, R, B float32 }

// textAlign is a DWRITE_TEXT_ALIGNMENT.
type textAlign uint32

const (
	alignLeading textAlign = 0
	alignCenter  textAlign = 2
)

// fontSpec selects a text format.
type fontSpec struct {
	family string
	size   float32 // pixels
	weight uint32  // DWRITE_FONT_WEIGHT
	align  textAlign
}

// d2d holds the factories, the render target and the formats.
type d2d struct {
	factory, dwrite, rt, brush *comObj
	formats                    map[fontSpec]*comObj
}

var d2dShared *d2d

// getD2D returns the shared renderer, creating it on first use.
func getD2D() (*d2d, error) {
	if d2dShared != nil {
		return d2dShared, nil
	}
	if err := d2d1.Load(); err != nil {
		return nil, err
	}
	g := &d2d{formats: map[fontSpec]*comObj{}}
	r, _, _ := procD2D1CreateFactory.Call(0 /* SINGLE_THREADED */, uintptr(unsafe.Pointer(&iidID2D1Factory)), 0, uintptr(unsafe.Pointer(&g.factory)))
	if err := hresult(r, "D2D1CreateFactory"); err != nil {
		return nil, err
	}
	r, _, _ = procDWriteCreateFactor.Call(0 /* SHARED */, uintptr(unsafe.Pointer(&iidIDWriteFactory)), uintptr(unsafe.Pointer(&g.dwrite)))
	if err := hresult(r, "DWriteCreateFactory"); err != nil {
		g.factory.release()
		return nil, err
	}
	props := struct {
		Type, Format, AlphaMode uint32
		DpiX, DpiY              float32
		Usage, MinLevel         uint32
	}{Type: 1 /* SOFTWARE */, Format: 87 /* B8G8R8A8_UNORM */, AlphaMode: 1 /* PREMULTIPLIED */, DpiX: 96, DpiY: 96}
	if err := hresult(g.factory.call(d2dFactoryCreateDCRenderTarget, uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&g.rt))), "CreateDCRenderTarget"); err != nil {
		g.dwrite.release()
		g.factory.release()
		return nil, err
	}
	g.rt.call(rtSetTextAntialiasMode, 2 /* GRAYSCALE: no ClearType on transparency */)
	white := colorF{1, 1, 1, 1}
	if err := hresult(g.rt.call(rtCreateSolidColorBrush, uintptr(unsafe.Pointer(&white)), 0, uintptr(unsafe.Pointer(&g.brush))), "CreateSolidColorBrush"); err != nil {
		g.rt.release()
		g.dwrite.release()
		g.factory.release()
		return nil, err
	}
	d2dShared = g
	return g, nil
}

// format returns the text format for f: one line, vertically centered,
// trimmed with an ellipsis.
func (g *d2d) format(f fontSpec) *comObj {
	if tf := g.formats[f]; tf != nil {
		return tf
	}
	var tf *comObj
	family, _ := windows.UTF16PtrFromString(f.family)
	locale, _ := windows.UTF16PtrFromString("en-us")
	r := g.dwrite.call(dwCreateTextFormat, uintptr(unsafe.Pointer(family)), 0, uintptr(f.weight), 0 /* NORMAL */, 5, /* NORMAL */
		uintptr(math.Float32bits(f.size)), uintptr(unsafe.Pointer(locale)), uintptr(unsafe.Pointer(&tf)))
	if hresult(r, "CreateTextFormat") != nil {
		return nil
	}
	tf.call(tfSetTextAlignment, uintptr(f.align))
	tf.call(tfSetParagraphAlignment, 2 /* CENTER */)
	tf.call(tfSetWordWrapping, 1 /* NO_WRAP */)
	var sign *comObj
	if g.dwrite.call(dwCreateEllipsisTrimmingSign, uintptr(unsafe.Pointer(tf)), uintptr(unsafe.Pointer(&sign))) == 0 {
		trim := struct{ Granularity, Delimiter, Count uint32 }{Granularity: 1 /* CHARACTER */}
		tf.call(tfSetTrimming, uintptr(unsafe.Pointer(&trim)), uintptr(unsafe.Pointer(sign)))
		sign.release() // the format keeps it
	}
	g.formats[f] = tf
	return tf
}

// measure returns the width of s in f, in pixels.
func (g *d2d) measure(s string, f fontSpec) float32 {
	tf := g.format(f)
	text, err := windows.UTF16FromString(s)
	if tf == nil || err != nil || len(text) <= 1 {
		return 0
	}
	var layout *comObj
	r := g.dwrite.call(dwCreateTextLayout, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), uintptr(unsafe.Pointer(tf)),
		uintptr(math.Float32bits(1e5)), uintptr(math.Float32bits(1e4)), uintptr(unsafe.Pointer(&layout)))
	if hresult(r, "CreateTextLayout") != nil {
		return 0
	}
	defer layout.release()
	var m struct {
		Left, Top, Width, WidthTrailing, Height, LayoutW, LayoutH float32
		MaxBidi, Lines                                            uint32
	}
	if layout.call(tlGetMetrics, uintptr(unsafe.Pointer(&m))) != 0 {
		return 0
	}
	return m.Width
}

// surface is a top-down 32-bit DIB with premultiplied alpha, BGRA.
type surface struct {
	bmp  uintptr
	bits []uint8
	w, h int32
}

func newSurface(w, h int32) (*surface, error) {
	if w <= 0 || h <= 0 {
		return nil, errors.New("empty surface")
	}
	hdr := struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
	}{Width: w, Height: -h, Planes: 1, BitCount: 32}
	hdr.Size = uint32(unsafe.Sizeof(hdr))
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil, errors.New("CreateDIBSection failed")
	}
	return &surface{bmp: bmp, bits: unsafe.Slice((*uint8)(bits), int(w)*int(h)*4), w: w, h: h}, nil
}

func (s *surface) free() {
	if s != nil && s.bmp != 0 {
		procDeleteObject.Call(s.bmp)
		s.bmp, s.bits = 0, nil
	}
}

// painter draws on a surface between begin and end.
type painter struct {
	g        *d2d
	s        *surface
	dc, prev uintptr
}

// begin binds the render target to s and clears it to transparent.
func (g *d2d) begin(s *surface) (*painter, error) {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	prev, _, _ := procSelectObject.Call(dc, s.bmp)
	r := rect{0, 0, s.w, s.h}
	if err := hresult(g.rt.call(dcrtBindDC, dc, uintptr(unsafe.Pointer(&r))), "BindDC"); err != nil {
		procSelectObject.Call(dc, prev)
		procDeleteDC.Call(dc)
		return nil, err
	}
	g.rt.call(rtBeginDraw)
	clear := colorF{}
	g.rt.call(rtClear, uintptr(unsafe.Pointer(&clear)))
	return &painter{g: g, s: s, dc: dc, prev: prev}, nil
}

// end finishes drawing; the surface's bits hold the result afterwards.
func (p *painter) end() error {
	err := hresult(p.g.rt.call(rtEndDraw, 0, 0), "EndDraw")
	procGdiFlush.Call()
	procSelectObject.Call(p.dc, p.prev)
	procDeleteDC.Call(p.dc)
	return err
}

func (p *painter) color(c colorF) {
	p.g.brush.call(brushSetColor, uintptr(unsafe.Pointer(&c)))
}

func (p *painter) fillRect(r rectF, c colorF) {
	p.color(c)
	p.g.rt.call(rtFillRectangle, uintptr(unsafe.Pointer(&r)), uintptr(unsafe.Pointer(p.g.brush)))
}

func (p *painter) fillRound(r rectF, radius float32, c colorF) {
	p.color(c)
	rr := struct {
		R      rectF
		RX, RY float32
	}{r, radius, radius}
	p.g.rt.call(rtFillRoundedRectangle, uintptr(unsafe.Pointer(&rr)), uintptr(unsafe.Pointer(p.g.brush)))
}

func (p *painter) fillCircle(cx, cy, radius float32, c colorF) {
	p.color(c)
	e := struct{ X, Y, RX, RY float32 }{cx, cy, radius, radius}
	p.g.rt.call(rtFillEllipse, uintptr(unsafe.Pointer(&e)), uintptr(unsafe.Pointer(p.g.brush)))
}

// text draws s in r, clipped and trimmed, in color (emoji in their own).
func (p *painter) text(s string, r rectF, f fontSpec, c colorF) {
	tf := p.g.format(f)
	text, err := windows.UTF16FromString(s)
	if tf == nil || err != nil || len(text) <= 1 {
		return
	}
	p.color(c)
	p.g.rt.call(rtDrawText, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), uintptr(unsafe.Pointer(tf)),
		uintptr(unsafe.Pointer(&r)), uintptr(unsafe.Pointer(p.g.brush)), 2|4 /* CLIP | ENABLE_COLOR_FONT */, 0 /* NATURAL */)
}

var procGdiFlush = gdi32.NewProc("GdiFlush")
