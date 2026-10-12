// Package assets embeds the brand images the app shows at runtime. The
// exe's icon (icon.ico) is linked separately by go-winres.
package assets

import _ "embed"

// LogoPNG is the app logo (512×512), shown next to the title in the panel.
//
//go:embed resound.png
var LogoPNG []byte
