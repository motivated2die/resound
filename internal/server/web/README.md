# Phone remote (web app)

The remote that phones (and the PC's browser) open. It started as the Soundux Remote front end and is maintained here now. Don't copy the old project over it.

Everything in this folder is embedded into `resound.exe` at build time. Without an `index.html`, the server shows a short placeholder page instead.

- **Look:** the colours in `css/styles.css` (`:root`) and `login.html` match the desktop panel (`internal/ui/canvas_windows.go`): background `#1c1d22`, accent `#50dea8` with dark text `#102a20` on it, berry red `#f2416f`. Change both together.
- **App icons:** `assets/resound-192.png`, `-512.png` and `favicon.ico` are generated from the repo's `assets/` folder. Run `go run ./scripts/icons` after changing the logo.
- **UI icons:** Material Symbols SVGs in `assets/icons/`, used as `<span class="icon" data-icon="name"></span>`; see `assets/icons/README.md` for adding one.
- **Nothing external.** Libraries live in `vendor/` (each with its license and a README naming the source), and the server's Content-Security-Policy blocks other hosts. Don't add CDN links; vendor the file instead. `go test ./internal/server` fails on URLs in our own web files.
- **Trying changes** without the desktop app or audio: `go run ./cmd/devremote -folder <sounds>`. It prints the PIN and a pairing link. Rebuild after editing, because files are embedded.
