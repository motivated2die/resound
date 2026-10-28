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
| `index.html`, `js/landing.js`, `css/landing.css` | The landing page at `/`: what Resound is, the download, getting started (with cards for switching from Soundpad and Soundux), features, the desktop panel (an animated picture, icons in `assets/panel-icons/`), the launchpad's and the overlay's live demos (`js/launchpad-demo.js`, `js/overlay-demo.js`), how it works, news (the latest two entries open). The installed app skips it |
| `app.html`, `js/app.js`, `js/*.js`, `css/styles.css` | The remote (the soundboard): tabs, sound cards, layouts, settings. The installed app starts here |
| `js/rtc.js` | The connection: pairing, reconnecting, `fetch()` for `/api/` over the data channel, and events from the PC (`onEvent`, e.g. `library`: `app.js` reloads tabs and sounds). The first script on every page |
| `connect.html`, `js/connect.js`, `css/connect.css` | Pairing (link, in-app QR scanner, pasted link) and the list of paired PCs |
| `js/connection-help.js`, `css/connection-help.css` | What to check when the PC doesn't answer, on the remote and the connect page. Keep its steps in line with the panel's Phone remote page (`internal/app/pages.go`) |
| `css/scrollbar.css` | Scrollbars on every page, the help center's too: no track or arrows, a slim thumb that widens on hover |
| `js/screen-wake.js` | Keeps the screen on (Screen Wake Lock API), on by default |
| `js/fullscreen.js` | Fullscreen in a browser tab, on by default on phones and tablets only; the installed app opens fullscreen by itself (`manifest.json`) |
| `js/platform.js` | What kind of device this is, before the first paint: iPhones and iPads (their status bar never hides, so the top bar moves below it) and phones and tablets in general (fullscreen by default) |
| `js/device.js` | Vibration on play and stop, persistent storage for the pairing keys |
| `js/sort.js` | Sorting a tab in edit mode (custom order, A–Z, Z–A by name), saved per tab on the PC; the custom order is never changed by it |
| `js/search.js` | Search across all tabs: fuzzy by name, a colour filter; opened from the top bar or by pulling down with two fingers. Results fill the usual grid; the bottom bar stays above the virtual keyboard |
| `js/install.js` | The "Install app" pill in the top bar and its sheet: why, and how in this browser (one tap where Chromium offers it, steps elsewhere) |
| `manifest.json`, `sw.js` | Installation and offline start; the build fills in the service worker's file list (the help center is left out) |
| `_headers` | Response headers for Cloudflare Pages (CSP: nothing from other hosts) |
| `robots.txt` | For search engines: everything may be crawled, and where the sitemap is. The build adds `sitemap.xml` (the landing page and every help article); the app's own pages are marked `noindex` instead of being blocked. Neither is cached by the app |
| `vendor/` | Libraries, each with its license and a README naming the source |

- **Look:** the colours in `css/styles.css` (`:root`) and `css/connect.css` match the desktop panel (`internal/ui/canvas_windows.go`): background `#1c1d22`, accent `#50dea8` with dark text `#102a20` on it, berry red `#f2416f`. Change them together.
- **App icons:** `assets/resound-192.png`, `-512.png`, `resound-maskable-512.png` and `favicon.ico` are generated from the repo's `assets/` folder. Run `go run ./scripts/icons` after changing the logo.
- **UI icons:** Material Symbols SVGs in `assets/icons/`, used as `<span class="icon" data-icon="name"></span>`; see `assets/icons/README.md` for adding one.
- **Link previews and search:** `index.html` and the help center's `page.html` carry a canonical URL and Open Graph tags (title, description, and `assets/preview.webp`, a 1200 x 630 picture of the landing page, shown large; the installed app doesn't cache it), which Discord and other apps show when a link is pasted. They need absolute URLs on `https://getresound.app` (`site` in `cmd/webapp`); check a change with a preview tool or by pasting the link into a private Discord channel after deploying.
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
| `help.css`, `help.js` | Styles (the landing page's palette) and behaviour: topics drawer, search, checklists, visualization switches, "Ask an AI" |
| `ai-prompt.txt` | The instructions for an AI assistant that "Ask an AI" hands over: `{{documentation}}` marks where the documentation goes, and it ends with "My question" and an empty line for the reader |

Writing articles, in GitHub Markdown:

- Link to other articles by file: `[Set up voice chat](voice-chat.md)`, `[…](discord.md#input-sensitivity)` (headings get IDs from their text). Site pages as `/connect`, the web as `https://…`.
- Callouts: `:::tip`, `:::note` or `:::warning`, an optional title after it, the text, then `:::` on its own line.
- Visualizations: `:::viz audio-flow`, a caption, `:::`. Use them sparingly, where a picture explains what text can't.
- Checklists: a list of `- [ ] Step` items becomes boxes readers tick off (kept only while the page is open). For troubleshooting steps.
- Keys as `<kbd>Esc</kbd>`, UI labels in **bold**, paths in the panel as Settings → Audio & voice chat.

### Ask an AI

The overview (right column, or below the intro on narrower screens) and the end of every article have an **Ask an AI** card: a copy button, and below it a menu of assistants to open. It works without any integration with the AI providers; they are plain links:

- **Copy for your AI** copies `ai-prompt.txt` with every article as Markdown in its slot (`/help/ai-context.txt`, about 85 KB), to paste into any AI chat and write the question below.
- **ChatGPT, Claude, Perplexity, Grok** open with the prompt in their address (`?q=`), with a link to `/llms-full.txt` in the slot that they read with their web tools. The whole help center is far too long for an address. The text is copied too, in case the assistant can't open the link.
- **Gemini, Copilot** take no prompt in their address: they open empty, and the reader pastes what was copied.

The build also writes the help center for AI assistants and tools ([llmstxt.org](https://llmstxt.org)): `/llms.txt` (the index), `/llms-full.txt` (everything) and `/help/<article>.md`. Links become absolute, callouts become quotes, and visualizations keep only their caption. Assistants and their address parameters are `aiAssistants` in `cmd/webapp/llms.go`; when a provider changes its parameter, change it there. `_headers` serves these files as UTF-8 text.

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
