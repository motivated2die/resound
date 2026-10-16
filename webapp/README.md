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
| `index.html`, `js/landing.js`, `css/landing.css` | The landing page at `/`: what Resound is, the download, getting started, features, the desktop panel (an animated picture, icons in `assets/panel-icons/`), how it works, news. The installed app skips it |
| `app.html`, `js/app.js`, `js/*.js`, `css/styles.css` | The remote (the soundboard): tabs, sound cards, layouts, settings. The installed app starts here |
| `js/rtc.js` | The connection: pairing, reconnecting, `fetch()` for `/api/` over the data channel, and events from the PC (`onEvent`, e.g. `library`: `app.js` reloads tabs and sounds). The first script on every page |
| `connect.html`, `js/connect.js`, `css/connect.css` | Pairing (link, in-app QR scanner, pasted link) and the list of paired PCs |
| `js/connection-help.js`, `css/connection-help.css` | What to check when the PC doesn't answer, on the remote and the connect page. Keep its steps in line with the panel's Phone remote page (`internal/app/pages.go`) |
| `css/scrollbar.css` | Scrollbars on every page, the help center's too: no track or arrows, a slim thumb that widens on hover |
| `js/screen-wake.js` | Keeps the screen on (Screen Wake Lock API), on by default |
| `js/fullscreen.js` | Fullscreen in a browser tab; the installed app opens fullscreen by itself (`manifest.json`) |
| `js/device.js` | Vibration on play and stop, persistent storage for the pairing keys |
| `js/search.js` | Search across all tabs: fuzzy by name, a colour filter; opened from the top bar or by pulling down with two fingers. Results fill the usual grid; the bottom bar stays above the virtual keyboard |
| `js/install.js` | The "Install app" pill in the top bar and its sheet: why, and how in this browser (one tap where Chromium offers it, steps elsewhere) |
| `manifest.json`, `sw.js` | Installation and offline start; the build fills in the service worker's file list (the help center is left out) |
| `_headers` | Response headers for Cloudflare Pages (CSP: nothing from other hosts) |
| `vendor/` | Libraries, each with its license and a README naming the source |

- **Look:** the colours in `css/styles.css` (`:root`) and `css/connect.css` match the desktop panel (`internal/ui/canvas_windows.go`): background `#1c1d22`, accent `#50dea8` with dark text `#102a20` on it, berry red `#f2416f`. Change them together.
- **App icons:** `assets/resound-192.png`, `-512.png`, `resound-maskable-512.png` and `favicon.ico` are generated from the repo's `assets/` folder. Run `go run ./scripts/icons` after changing the logo.
- **UI icons:** Material Symbols SVGs in `assets/icons/`, used as `<span class="icon" data-icon="name"></span>`; see `assets/icons/README.md` for adding one.
- **Nothing external.** Libraries live in `vendor/`, and the Content-Security-Policy blocks other hosts. Don't add CDN links; vendor the file instead. `go test ./cmd/webapp` fails on URLs in our own files and on icons without SVG or CSS rule.

## Help center (`help/`)

The help center at `/help/`: setup guides, features, troubleshooting, FAQ and uninstalling. It is part of the website but not of the installed app (not cached, no new app version when it changes). `cmd/webapp` renders it into `dist/help/`.

| File | Purpose |
|---|---|
| `SUMMARY.md` | The sidebar, in reading order: `## Section`, then `- [Label](article.md)` per article. Every article must be listed once |
| `index.md` | The overview at `/help/`; the sections and their articles are added below it |
| `*.md` | One article each, at `/help/<file name>`. Starts with its `# Title`; the first paragraph is its summary (search, meta description) |
| `viz/*.html` | Small visualizations, inserted where an article says `:::viz <name>` |
| `page.html` | The page template (Go `html/template`) |
| `help.css`, `help.js` | Styles (the landing page's palette) and behaviour: topics drawer, search, checklists, visualization switches |

Writing articles, in GitHub Markdown:

- Link to other articles by file: `[Set up voice chat](voice-chat.md)`, `[…](discord.md#input-sensitivity)` (headings get IDs from their text). Site pages as `/connect`, the web as `https://…`.
- Callouts: `:::tip`, `:::note` or `:::warning`, an optional title after it, the text, then `:::` on its own line.
- Visualizations: `:::viz audio-flow`, a caption, `:::`. Use them sparingly, where a picture explains what text can't.
- Checklists: a list of `- [ ] Step` items becomes boxes readers tick off (kept only while the page is open). For troubleshooting steps.
- Keys as `<kbd>Esc</kbd>`, UI labels in **bold**, paths in the panel as Settings → Audio & voice chat.

The build fails on broken links and headings, unknown visualizations and articles missing from `SUMMARY.md`; `go test ./cmd/webapp` runs it.

## Build and try

```powershell
go run ./cmd/webapp                         # builds webapp/dist (git-ignored)
go run ./cmd/webapp -serve :8095            # builds and serves it on http://localhost:8095 (help: /help/)
go run ./cmd/devremote -folder D:\Sounds    # the PC side; prints a pairing link for localhost:8095; Enter refreshes the library
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
foreground (or within the hour while on screen): the service worker fetches
it, and the page reloads into it as soon as that interrupts nobody, at the
latest when the app goes to the background.
