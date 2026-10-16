package ui

// Icons and images. Both are rasterized here in pure Go, anti-aliased, at
// the exact device-pixel size they are drawn at, so they stay crisp at any
// DPI. Drawing (alpha-blending onto the panel) is in icon_windows.go.
//
// Icons are SVG files in icons/, embedded at build time; add more with
// scripts/add-icon.ps1 (Tabler or Font Awesome). They are drawn as masks in
// any color, so an icon follows its button's text color.

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png" // NewImage decodes PNG
	"io/fs"
	"math"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

//go:embed icons/*.svg
var iconFS embed.FS

// Icon names an embedded SVG: icons/<name>.svg. The zero value is no icon.
type Icon string

// Icons used by the app. Any file in icons/ also works as Icon("name").
const (
	IconAlert        Icon = "alert-triangle"
	IconCheck        Icon = "check"
	IconChevronLeft  Icon = "chevron-left"
	IconClose        Icon = "x"
	IconCopy         Icon = "copy"
	IconChevronRight Icon = "chevron-right"
	IconDownload     Icon = "download"
	IconExternalLink Icon = "external-link"
	IconFolder       Icon = "folder"
	IconFolderPlus   Icon = "folder-plus"
	IconHandGrab     Icon = "hand-grab"
	IconHelp         Icon = "help-circle"
	IconInfo         Icon = "info-circle"
	IconKeyboard     Icon = "keyboard"
	IconMic          Icon = "microphone"
	IconMicOff       Icon = "microphone-off"
	IconPause        Icon = "player-pause"
	IconPlay         Icon = "player-play"
	IconPower        Icon = "power"
	IconUnplugged    Icon = "plug-connected-x"
	IconRefresh      Icon = "refresh"
	IconRestore      Icon = "restore"
	IconSave         Icon = "device-floppy"
	IconSettings     Icon = "settings"
	IconStop         Icon = "player-stop"
	IconTrash        Icon = "trash"
)

// rasterIcon renders an icon as an alpha mask of size×size pixels. The
// view box is scaled to fit and centred, keeping its aspect ratio (Font
// Awesome icons are often wider than tall).
func rasterIcon(name Icon, size int) (*image.Alpha, error) {
	data, err := iconFS.ReadFile("icons/" + string(name) + ".svg")
	if err != nil {
		return nil, fmt.Errorf("icon %q: %w", name, err)
	}
	mask, err := rasterSVG(data, size)
	if err != nil {
		return nil, fmt.Errorf("icon %q: %w", name, err)
	}
	return mask, nil
}

func rasterSVG(data []byte, size int) (*image.Alpha, error) {
	// Tabler strokes with currentColor; any opaque color gives the mask.
	data = bytes.ReplaceAll(data, []byte("currentColor"), []byte("#000"))
	svg, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, err
	}
	w, h := svg.ViewBox.W, svg.ViewBox.H
	if w <= 0 || h <= 0 || size <= 0 {
		return nil, fmt.Errorf("empty view box")
	}
	s := float64(size) / max(w, h)
	tw, th := w*s, h*s
	svg.SetTarget((float64(size)-tw)/2, (float64(size)-th)/2, tw, th)

	rgba := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, rgba, rgba.Bounds())
	svg.Draw(rasterx.NewDasher(size, size, scanner), 1)
	mask := image.NewAlpha(rgba.Bounds())
	for i := range mask.Pix {
		mask.Pix[i] = rgba.Pix[4*i+3]
	}
	return mask, nil
}

// iconNames lists every embedded icon, for tests.
func iconNames() []Icon {
	var out []Icon
	_ = fs.WalkDir(iconFS, "icons", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".svg") {
			out = append(out, Icon(strings.TrimSuffix(strings.TrimPrefix(path, "icons/"), ".svg")))
		}
		return nil
	})
	return out
}

// Image is a bitmap (PNG) shown in the panel, e.g. the app logo. It is
// decoded once and scaled with a high-quality filter per drawn size.
type Image struct {
	src    image.Image
	scaled map[int]*image.RGBA
}

// NewImage decodes a PNG.
func NewImage(data []byte) (*Image, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return &Image{src: src, scaled: map[int]*image.RGBA{}}, nil
}

// at returns the image scaled to fit size×size, centred, with
// premultiplied alpha (as image.RGBA stores it).
func (im *Image) at(size int) *image.RGBA {
	if out, ok := im.scaled[size]; ok {
		return out
	}
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	b := im.src.Bounds()
	w, h := size, size
	if b.Dx() > b.Dy() {
		h = size * b.Dy() / b.Dx()
	} else {
		w = size * b.Dx() / b.Dy()
	}
	x, y := (size-w)/2, (size-h)/2
	xdraw.CatmullRom.Scale(out, image.Rect(x, y, x+w, y+h), im.src, b, draw.Over, nil)
	im.scaled[size] = out
	return out
}

// dashedRoundRect renders the dashed outline of a w×h rounded rectangle as
// an alpha mask, anti-aliased, with the dashes following the corners. The
// dash and gap lengths are stretched a little so the pattern closes evenly
// around the shape instead of ending in a short stub.
func dashedRoundRect(w, h int, radius, width, dash, gap float64) *image.Alpha {
	half := width / 2
	rr := max(radius-half, 0)
	inW, inH := float64(w)-width, float64(h)-width
	perimeter := 2*(inW+inH) - 8*rr + 2*math.Pi*rr
	n := max(1, math.Round(perimeter/(dash+gap)))
	k := perimeter / (n * (dash + gap))

	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())
	scanner.SetColor(color.Black)
	d := rasterx.NewDasher(w, h, scanner)
	d.SetStroke(fixed.Int26_6(width*64), 4*64, rasterx.ButtCap, nil, rasterx.RoundGap, rasterx.Round,
		[]float64{dash * k, gap * k}, 0)
	rasterx.AddRoundRect(half, half, float64(w)-half, float64(h)-half, rr, rr, 0, rasterx.RoundGap, d)
	d.Draw()
	mask := image.NewAlpha(rgba.Bounds())
	for i := range mask.Pix {
		mask.Pix[i] = rgba.Pix[4*i+3]
	}
	return mask
}
