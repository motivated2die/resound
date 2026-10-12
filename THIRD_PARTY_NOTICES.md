# Third-party notices

Resound is licensed under the [GNU General Public License v3.0](LICENSE). It includes or uses the following third-party components, each under its own license. All of them are compatible with GPL-3.0.

## Go modules (compiled into Resound.exe)

| Module | License |
|---|---|
| [github.com/go-ole/go-ole](https://github.com/go-ole/go-ole) | MIT |
| [github.com/gopxl/beep](https://github.com/gopxl/beep) | MIT |
| [github.com/hajimehoshi/go-mp3](https://github.com/hajimehoshi/go-mp3) | Apache-2.0 |
| [github.com/icza/bitio](https://github.com/icza/bitio) | Apache-2.0 |
| [github.com/jfreymuth/oggvorbis](https://github.com/jfreymuth/oggvorbis) | MIT |
| [github.com/jfreymuth/vorbis](https://github.com/jfreymuth/vorbis) | MIT |
| [github.com/mewkiz/flac](https://github.com/mewkiz/flac) | Unlicense (public domain) |
| [github.com/mewkiz/pkg](https://github.com/mewkiz/pkg) | Unlicense (public domain) |
| [github.com/moutend/go-wca](https://github.com/moutend/go-wca) | MIT |
| [github.com/pkg/errors](https://github.com/pkg/errors) | BSD-2-Clause |
| [github.com/skip2/go-qrcode](https://github.com/skip2/go-qrcode) | MIT |
| [github.com/srwiley/oksvg](https://github.com/srwiley/oksvg) | BSD-3-Clause |
| [github.com/srwiley/rasterx](https://github.com/srwiley/rasterx) | BSD-3-Clause |
| [golang.org/x/image](https://pkg.go.dev/golang.org/x/image), [x/net](https://pkg.go.dev/golang.org/x/net), [x/sys](https://pkg.go.dev/golang.org/x/sys), [x/text](https://pkg.go.dev/golang.org/x/text) | BSD-3-Clause |
| The Go standard library and runtime | BSD-3-Clause |

The full license texts are in each module's source (`go mod download`, then the module's folder in the Go module cache).

## Phone remote (embedded in Resound.exe)

| Component | License | Location |
|---|---|---|
| [driver.js](https://github.com/kamranahmedse/driver.js) | MIT | `internal/server/web/vendor/driverjs/` |
| [SortableJS](https://github.com/SortableJS/Sortable) 1.15.6 | MIT | `internal/server/web/vendor/sortablejs/` |
| [NoSleep.js](https://github.com/richtr/NoSleep.js) 0.12.0 | MIT | `internal/server/web/vendor/nosleep/` |

Loaded by the phone's browser from public CDNs (not included in Resound; see [PRIVACY.md](PRIVACY.md)):

| Component | License |
|---|---|
| [Material Symbols](https://github.com/google/material-design-icons) (Google Fonts) | Apache-2.0 |
| [emoji-picker-element](https://github.com/nolanlawson/emoji-picker-element) and its emoji data ([Emojibase](https://emojibase.dev)) | Apache-2.0, MIT |

## Icons

- [Tabler Icons](https://tabler.io/icons) (`internal/ui/icons/`): MIT, © Paweł Kuna.

## Not included: VB-Cable

[VB-Audio Virtual Cable](https://vb-cable.com) is proprietary donationware by VB-Audio Software and is **not** part of Resound. When the user chooses to install it, Resound downloads the unmodified package from VB-Audio's server, verifies VB-Audio's digital signature, and runs VB-Audio's own setup. Resound credits VB-Audio and links to its donation page wherever the cable is set up, as VB-Audio's licensing terms ask.

## Prior work

Resound builds on ideas from [Soundux](https://github.com/Soundux/Soundux) (GPL-3.0) and the Soundux Remote, whose front end was the starting point of the phone remote. The desktop app is a new implementation.
