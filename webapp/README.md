# Installable app (hosted web app)

The phone remote as a web app on an HTTPS host, so phones can install it
(home screen, fullscreen, screen wake lock, offline start). It talks to the PC
directly over the local network through a WebRTC data channel; no server of
ours is involved after the page is loaded. See
[docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md#installable-app) for how the
connection works.

Nothing here is embedded into `Resound.exe`. The app **reuses the phone
remote** in `internal/server/web/` and lays the files in `src/` over it:

| File | Purpose |
|---|---|
| `src/js/rtc.js` | The connection: pairing, reconnecting, and `fetch()` for `/api/` over the data channel. Injected before the remote's scripts in `index.html` |
| `src/connect.html`, `js/connect.js`, `css/connect.css` | Pairing (link, in-app QR scanner, pasted link) and the list of paired PCs |
| `src/login.html` | Replaces the PIN page: forwards to the connect page |
| `src/manifest.json`, `src/sw.js` | Install and offline start; the build fills in the service worker's file list |
| `src/_headers` | Netlify headers (CSP: nothing from other hosts) |
| `src/vendor/jsqr/` | QR decoder for browsers without `BarcodeDetector` (Safari, Firefox) |

Change the remote in `internal/server/web/`; both the PC's page and this app
get it. Code only for the app goes in `src/`.

## Build and try

```powershell
go run ./cmd/webapp                     # builds webapp/dist (git-ignored)
go run ./cmd/webapp -serve :8095        # builds and serves it on http://localhost:8095
go run ./cmd/devremote -folder D:\Sounds   # prints a pairing link for http://localhost:8095
```

`localhost` is a secure context, so the app works there in a desktop browser.
Phones need the HTTPS deployment.

## Deploy (Netlify)

`netlify.toml` in the repository root builds and publishes `webapp/dist`:
connect the repository in Netlify, nothing else to set. The site is currently
`https://getresound.netlify.app`, which is the default of `app.AppURL` (the
address in the PC's QR code). For another deployment, set it at build time
(`-ldflags "-X github.com/motivated2die/resound/internal/app.AppURL=https://…"`),
or on one PC with the `RESOUND_APP_URL` environment variable or `appURL` in
`config.json`.
