# Panel icons

SVG files in this folder are embedded into the exe (`go:embed icons/*.svg` in `../icon.go`). Nothing is loaded at runtime.

## Adding an icon

```powershell
.\scripts\add-icon.ps1 refresh, player-play          # Tabler outline (default)
.\scripts\add-icon.ps1 -Set tabler-filled star       # -> star-filled.svg
.\scripts\add-icon.ps1 -Set fa-solid volume-high     # -> fa-solid-volume-high.svg
```

Then use it by file name, `ui.Icon("player-play")`, or add a constant next to the others in `icon.go`, and rebuild. `go test ./internal/ui` renders every icon here at several sizes and checks the constants point at existing files.

Browse: [tabler.io/icons](https://tabler.io/icons) · [fontawesome.com (free)](https://fontawesome.com/search?ic=free). The script pins Tabler 3.34.0 and Font Awesome Free 6.7.2.

## Where icons can go

- `ui.Action{Icon: …}` in a `ui.Buttons` row: icon before the label, or alone with an empty label.
- `ui.NavTool(icon, tip, onTool, …)`: a square icon button left of a Nav tile.
- Inside new rows: `c.icon(name, box, sizeL, color)` draws centred in `box`.

## How they are drawn

Each icon is rasterized in pure Go (oksvg/rasterx), anti-aliased, at the exact device-pixel size for the monitor's DPI, as an alpha mask. It is tinted with the row's text color and alpha-blended onto the panel (`AlphaBlend`), then cached per name, size and color. Icons therefore stay sharp at 100–200 % scaling and follow hover and disabled colors.

Any single-color SVG works: stroked (Tabler, `currentColor`) or filled (Font Awesome). The view box keeps its aspect ratio and is centred. Gradients and multiple colors are ignored, because icons are drawn as masks.

## Licenses

- Tabler Icons: MIT, © Paweł Kuna.
- Font Awesome Free icons: CC BY 4.0. Their license comment is kept inside each SVG, which is the attribution Font Awesome asks for.
