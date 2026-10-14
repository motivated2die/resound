# Third-party notices

Resound is licensed under the [GNU General Public License v3.0](LICENSE). It includes or uses the following third-party components, each under its own license. All of them are compatible with GPL-3.0.

## Go modules (compiled into Resound.exe)

| Module | License |
|---|---|
| [github.com/go-ole/go-ole](https://github.com/go-ole/go-ole) | MIT |
| [github.com/google/uuid](https://github.com/google/uuid) | BSD-3-Clause |
| [github.com/gopxl/beep](https://github.com/gopxl/beep) | MIT |
| [github.com/hajimehoshi/go-mp3](https://github.com/hajimehoshi/go-mp3) | Apache-2.0 |
| [github.com/icza/bitio](https://github.com/icza/bitio) | Apache-2.0 |
| [github.com/jfreymuth/oggvorbis](https://github.com/jfreymuth/oggvorbis) | MIT |
| [github.com/jfreymuth/vorbis](https://github.com/jfreymuth/vorbis) | MIT |
| [github.com/mewkiz/flac](https://github.com/mewkiz/flac) | Unlicense (public domain) |
| [github.com/mewkiz/pkg](https://github.com/mewkiz/pkg) | Unlicense (public domain) |
| [github.com/moutend/go-wca](https://github.com/moutend/go-wca) | MIT |
| [github.com/pion/webrtc](https://github.com/pion/webrtc) and its modules (datachannel, dtls, ice, interceptor, logging, mdns, randutil, rtcp, rtp, sctp, sdp, srtp, stun, transport, turn) | MIT |
| [github.com/pkg/errors](https://github.com/pkg/errors) | BSD-2-Clause |
| [github.com/skip2/go-qrcode](https://github.com/skip2/go-qrcode) | MIT |
| [github.com/srwiley/oksvg](https://github.com/srwiley/oksvg) | BSD-3-Clause |
| [github.com/srwiley/rasterx](https://github.com/srwiley/rasterx) | BSD-3-Clause |
| [github.com/wlynxg/anet](https://github.com/wlynxg/anet) | BSD-3-Clause |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto), [x/image](https://pkg.go.dev/golang.org/x/image), [x/net](https://pkg.go.dev/golang.org/x/net), [x/sys](https://pkg.go.dev/golang.org/x/sys), [x/text](https://pkg.go.dev/golang.org/x/text), [x/time](https://pkg.go.dev/golang.org/x/time) | BSD-3-Clause |
| The Go standard library and runtime | BSD-3-Clause |

The full license texts are in each module's source (`go mod download`, then the module's folder in the Go module cache).

## Phone app (webapp/, hosted as a web page, not in Resound.exe)

| Component | License | Location |
|---|---|---|
| [driver.js](https://github.com/kamranahmedse/driver.js) | MIT | `webapp/src/vendor/driverjs/` |
| [SortableJS](https://github.com/SortableJS/Sortable) 1.15.6 | MIT | `webapp/src/vendor/sortablejs/` |
| [emoji-picker-element](https://github.com/nolanlawson/emoji-picker-element) 1.29.1 | Apache-2.0 | `webapp/src/vendor/emoji-picker-element/` |
| [emoji-picker-element-data](https://github.com/nolanlawson/emoji-picker-element-data) 1.8.0 (English, Emojibase shortcodes) | Apache-2.0; built from [Emojibase](https://emojibase.dev) data (MIT) with names and keywords from [Unicode CLDR](https://cldr.unicode.org) (Unicode License v3) | `webapp/src/vendor/emoji-picker-element-data/` |
| [jsQR](https://github.com/cozmo/jsQR) 1.4.0 | Apache-2.0 | `webapp/src/vendor/jsqr/` |
| [Material Symbols](https://github.com/google/material-design-icons) (Outlined) SVG icons by Google | Apache-2.0 | `webapp/src/assets/icons/` |
| [Tabler Icons](https://tabler.io/icons) (outline) SVG icons by Paweł Kuna, copies of the panel's | MIT | `webapp/src/assets/panel-icons/` |

Each folder contains the component's license file(s) and a README naming the exact source. The app loads nothing from other hosts.

## Icons

- [Tabler Icons](https://tabler.io/icons) (`internal/ui/icons/`): MIT, © Paweł Kuna.

## Not included: VB-Cable

[VB-Audio Virtual Cable](https://vb-cable.com) is proprietary donationware by VB-Audio Software and is **not** part of Resound. When the user chooses to install it, Resound downloads the unmodified package from VB-Audio's server, verifies VB-Audio's digital signature, and runs VB-Audio's own setup. Resound credits VB-Audio and links to its donation page wherever the cable is set up, as VB-Audio's licensing terms ask.

## Prior work

Resound builds on ideas from [Soundux](https://github.com/Soundux/Soundux) (GPL-3.0) and the Soundux Remote, whose front end was the starting point of the phone remote. The desktop app is a new implementation.
