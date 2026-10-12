// Command icons builds every icon file from the logos in assets/. Run it
// after changing a logo:
//
//	go run ./scripts/icons
//
// It writes:
//   - assets/icon.ico: the exe, Start Menu, taskbar and tray icon, with every
//     size Windows asks for at 100–200 % display scaling (16 to 96 px as
//     bitmaps, 256 px as PNG). Without these, Windows shrinks one large image
//     on the fly and the icon looks soft.
//   - internal/server/web/assets: resound-512.png and -192.png (copies)
//     and favicon.ico for the phone remote.
//
// Sizes are scaled from assets/resound.png (512 px) with a Catmull-Rom
// filter and a light sharpening at small sizes. A hand-made version wins:
// put assets/icon-<size>.png (e.g. icon-16.png, exactly that many pixels
// square) next to the logo and it is used as is.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

// appIconSizes are the sizes the Windows shell uses (Microsoft's icon
// guidelines, including the steps for 125/150/175 % scaling).
var appIconSizes = []int{16, 20, 24, 30, 32, 36, 40, 48, 60, 64, 72, 80, 96, 256}

var webIconSizes = []int{16, 32, 48, 64}

func main() {
	logo := decodePNG(filepath.Join("assets", "resound.png"))

	ico, err := buildICO(sized(logo, appIconSizes, true), false)
	check(err)
	check(os.WriteFile(filepath.Join("assets", "icon.ico"), ico, 0o644))
	fmt.Printf("wrote assets/icon.ico (%d sizes, %d KB)\n", len(appIconSizes), len(ico)/1024)

	web := filepath.Join("internal", "server", "web", "assets")
	for from, to := range map[string]string{"resound.png": "resound-512.png", "resound-192.png": "resound-192.png"} {
		data, err := os.ReadFile(filepath.Join("assets", from))
		check(err)
		check(os.WriteFile(filepath.Join(web, to), data, 0o644))
	}
	fav, err := buildICO(sized(logo, webIconSizes, false), true)
	check(err)
	check(os.WriteFile(filepath.Join(web, "favicon.ico"), fav, 0o644))
	fmt.Println("wrote", web, "resound-512.png, resound-192.png, favicon.ico")
}

type sizedImage struct {
	size int
	img  *image.RGBA
}

// sized returns the logo at each size; with overrides, assets/icon-<n>.png
// replaces the scaled version.
func sized(logo image.Image, sizes []int, overrides bool) []sizedImage {
	var out []sizedImage
	for _, s := range sizes {
		if overrides {
			p := filepath.Join("assets", fmt.Sprintf("icon-%d.png", s))
			if _, err := os.Stat(p); err == nil {
				img := decodePNG(p)
				if b := img.Bounds(); b.Dx() != s || b.Dy() != s {
					log.Fatalf("%s is %dx%d, want %dx%d", p, b.Dx(), b.Dy(), s, s)
				}
				rgba := image.NewRGBA(image.Rect(0, 0, s, s))
				draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
				fmt.Printf("  %d px: hand-made %s\n", s, p)
				out = append(out, sizedImage{s, rgba})
				continue
			}
		}
		out = append(out, sizedImage{s, scale(logo, s)})
	}
	return out
}

// scale downsizes with Catmull-Rom, then sharpens small sizes slightly to
// make up for the softening of strong reduction.
func scale(src image.Image, s int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, s, s))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	switch {
	case s <= 24:
		return sharpen(dst, 0.45)
	case s <= 48:
		return sharpen(dst, 0.3)
	case s <= 96:
		return sharpen(dst, 0.15)
	}
	return dst
}

// sharpen is an unsharp mask with a 3x3 blur, on premultiplied pixels (so
// colours never exceed their alpha).
func sharpen(src *image.RGBA, amount float64) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	w, h := b.Dx(), b.Dy()
	at := func(x, y, c int) float64 {
		x, y = min(max(x, 0), w-1), min(max(y, 0), h-1)
		return float64(src.Pix[y*src.Stride+x*4+c])
	}
	for y := range h {
		for x := range w {
			i := y*dst.Stride + x*4
			alpha := 0.0
			for c := 3; c >= 0; c-- { // alpha first: it caps the colours
				blur := 0.0
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						blur += at(x+dx, y+dy, c)
					}
				}
				blur /= 9
				v := at(x, y, c) + amount*(at(x, y, c)-blur)
				v = min(max(v, 0), 255)
				if c == 3 {
					alpha = v
				} else {
					v = min(v, alpha)
				}
				dst.Pix[i+c] = uint8(v + 0.5)
			}
		}
	}
	return dst
}

// buildICO stores the images in an ICO file. Sizes below 256 are 32-bit
// bitmaps, the format every part of Windows reads; 256 px is PNG to keep
// the file small. allPNG stores every size as PNG (fine for browsers).
func buildICO(images []sizedImage, allPNG bool) ([]byte, error) {
	var data [][]byte
	for _, im := range images {
		var b []byte
		var err error
		if allPNG || im.size >= 256 {
			var buf bytes.Buffer
			err = png.Encode(&buf, im.img)
			b = buf.Bytes()
		} else {
			b = dib(im.img)
		}
		if err != nil {
			return nil, err
		}
		data = append(data, b)
	}
	var out bytes.Buffer
	le := func(v any) { _ = binary.Write(&out, binary.LittleEndian, v) }
	le(uint16(0)) // reserved
	le(uint16(1)) // type: icon
	le(uint16(len(images)))
	offset := 6 + 16*len(images)
	for i, im := range images {
		dim := uint8(im.size % 256) // 0 means 256
		out.WriteByte(dim)
		out.WriteByte(dim)
		out.WriteByte(0) // palette colors
		out.WriteByte(0) // reserved
		le(uint16(1))    // color planes
		le(uint16(32))   // bits per pixel
		le(uint32(len(data[i])))
		le(uint32(offset))
		offset += len(data[i])
	}
	for _, b := range data {
		out.Write(b)
	}
	return out.Bytes(), nil
}

// dib encodes an icon bitmap: BITMAPINFOHEADER (height doubled for the AND
// mask), bottom-up BGRA rows with straight alpha, then an empty AND mask.
func dib(img *image.RGBA) []byte {
	s := img.Bounds().Dx()
	var out bytes.Buffer
	le := func(v any) { _ = binary.Write(&out, binary.LittleEndian, v) }
	maskRow := ((s + 31) / 32) * 4
	le(uint32(40))
	le(int32(s))
	le(int32(2 * s))
	le(uint16(1))
	le(uint16(32))
	le(uint32(0)) // BI_RGB
	le(uint32(s*s*4 + maskRow*s))
	le(int32(0))
	le(int32(0))
	le(uint32(0))
	le(uint32(0))
	for y := s - 1; y >= 0; y-- {
		for x := range s {
			i := y*img.Stride + x*4
			r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
			if a > 0 && a < 255 { // un-premultiply
				r = uint8(min(255, int(r)*255/int(a)))
				g = uint8(min(255, int(g)*255/int(a)))
				b = uint8(min(255, int(b)*255/int(a)))
			}
			out.Write([]byte{b, g, r, a})
		}
	}
	out.Write(make([]byte, maskRow*s)) // transparency comes from the alpha
	return out.Bytes()
}

func decodePNG(path string) image.Image {
	f, err := os.Open(path)
	check(err)
	defer f.Close()
	img, err := png.Decode(f)
	check(err)
	return img
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
