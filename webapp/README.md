# Resound app

The phone remote: a web app on an HTTPS host that phones (and the PC's
browser) open from the PC's QR code and install. It talks to the PC directly
over the local network through a WebRTC data channel; no server of ours is
involved after the page is loaded. See
[docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md#the-app-and-its-connection)
for how the connection works. It started as the Soundux Remote front end and
is maintained here; don't copy the old project over it.

Nothing here is embedded into `Resound.exe`.

## Files (`src/`)

| File | Purpose |
|---|---|
| `index.html`, `js/landing.js`, `css/landing.css` | The landing page at `/`: what Resound is, the download, getting started, features, how it works, news. The installed app skips it |
| `app.html`, `js/app.js`, `js/*.js`, `css/styles.css` | The remote (the soundboard): tabs, sound cards, layouts, settings. The installed app starts here |
| `js/rtc.js` | The connection: pairing, reconnecting, and `fetch()` for `/api/` over the data channel. The first script on every page |
| `connect.html`, `js/connect.js`, `css/connect.css` | Pairing (link, in-app QR scanner, pasted link) and the list of paired PCs |
| `js/screen-wake.js` | Keeps the screen on (Screen Wake Lock API), on by default |
| `js/fullscreen.js` | Fullscreen in a browser tab; the installed app opens fullscreen by itself (`manifest.json`) |
| `js/device.js` | Vibration on play and stop, "Install the app", persistent storage for the pairing keys |
| `manifest.json`, `sw.js` | Installation and offline start; the build fills in the service worker's file list |
| `_headers` | Response headers for Cloudflare Pages (CSP: nothing from other hosts) |
| `vendor/` | Libraries, each with its license and a README naming the source |

- **Look:** the colours in `css/styles.css` (`:root`) and `css/connect.css` match the desktop panel (`internal/ui/canvas_windows.go`): background `#1c1d22`, accent `#50dea8` with dark text `#102a20` on it, berry red `#f2416f`. Change them together.
- **App icons:** `assets/resound-192.png`, `-512.png`, `resound-maskable-512.png` and `favicon.ico` are generated from the repo's `assets/` folder. Run `go run ./scripts/icons` after changing the logo.
- **UI icons:** Material Symbols SVGs in `assets/icons/`, used as `<span class="icon" data-icon="name"></span>`; see `assets/icons/README.md` for adding one.
- **Nothing external.** Libraries live in `vendor/`, and the Content-Security-Policy blocks other hosts. Don't add CDN links; vendor the file instead. `go test ./cmd/webapp` fails on URLs in our own files and on icons without SVG or CSS rule.

## Build and try

```powershell
go run ./cmd/webapp                         # builds webapp/dist (git-ignored)
go run ./cmd/webapp -serve :8095            # builds and serves it on http://localhost:8095
go run ./cmd/devremote -folder D:\Sounds    # the PC side; prints a pairing link for localhost:8095
```

`localhost` is a secure context, so the app works there in a desktop browser.
Phones need the HTTPS deployment.

## Deploy (Cloudflare Pages)

The site is `https://getresound.app`, a Cloudflare Pages project with direct
uploads (no Git integration):

1. `go run ./cmd/webapp`
2. Cloudflare dashboard → Workers & Pages → the project → **Create deployment**
   → Production → drop the `webapp/dist` folder → **Save and Deploy**.

`_headers` sets the security headers there. Pages serves `app.html` at
`/app` (and redirects `/app.html` there); the app only links the short
URLs.

`https://getresound.app` is the default of `app.AppURL`, the address in the
PC's QR code. For another deployment, set it at build time
(`-ldflags "-X github.com/motivated2die/resound/internal/app.AppURL=https://…"`),
or on one PC with the `RESOUND_APP_URL` environment variable or `appURL` in
`config.json`.

Installed apps pick up a new deployment the next time they come to the
foreground: the service worker fetches it and the page reloads.
