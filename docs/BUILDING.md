# Building Resound

How to set up, build, brand, test and debug. How versions and releases work: [RELEASING.md](RELEASING.md). For the design, see [ARCHITECTURE.md](ARCHITECTURE.md).

## Quick start

```powershell
go mod tidy
go build -o resound-debug.exe ./cmd/resound
.\resound-debug.exe
```

Builds inside the repo are portable and never install themselves. Click the tray icon to open the panel.

## Prerequisites

| Tool | Needed for | Install |
|---|---|---|
| Windows 10/11, x64 | everything (Windows-only app) | — |
| Go (the release pinned by `toolchain` in `go.mod`) | building | go.dev/dl; an older Go 1.24+ downloads the pinned release by itself |
| go-winres v0.3.3 | icon, manifest, version info | `go install github.com/tc-hib/go-winres@v0.3.3` |
| VB-Audio Virtual Cable | "Others hear" (runtime only) | vb-audio.com/Cable, or "Install VB-Cable" in the app |

No C compiler is needed: the whole project is pure Go.

## First-time setup

```powershell
git clone <repo> resound; cd resound
go mod tidy                                   # fetches modules, writes go.sum

# Optional: icon, DPI manifest, version info (see "Icon" below)
go-winres make --in winres\winres.json --out cmd\resound\rsrc --arch amd64
```

`go-winres make` writes `cmd\resound\rsrc_windows_amd64.syso`. Go links any `.syso` in the main package automatically. The file is git-ignored: regenerate it after a fresh clone, and whenever the icon or `winres.json` changes.

## Build variants

| Variant | Command | Result |
|---|---|---|
| Dev, with console | `go build -o resound-debug.exe ./cmd/resound` | Console window with live logs; portable |
| Dev, no console | `go build -ldflags "-H=windowsgui" -o resound.exe ./cmd/resound` | Like release, but portable while in the repo |
| Release | `.\scripts\build-release.ps1 -Version 0.7.0` | Vet, tests, resources and versions; `Resound.exe` and `SHA256SUMS.txt` on the Desktop. The release workflow runs the same script ([RELEASING.md](RELEASING.md)) |
| Release, by hand | `go build -trimpath -ldflags "-H=windowsgui -s -w -X github.com/motivated2die/resound/internal/app.Version=0.7.0" -o <outside-repo>\Resound.exe ./cmd/resound` | Same, without vet, tests or resources |

- **Portable vs. installing.** An exe never installs itself when a `go.mod` sits next to it or one folder up, when a file named `portable` sits next to it, or when `RESOUND_PORTABLE=1` is set. Builds in the repo are therefore always portable. A release exe in a normal folder installs itself on first start.
- **Release flags.** `-H=windowsgui` hides the console, `-s -w` strips debug data, and `-trimpath` removes local paths.
- **Start a debug build from an open terminal**, not by double-clicking. Crash output stays visible even if logging fails.

## Versioning

Three version numbers must agree in a release: `app.Version` (shown in the panel and compared by the installer), the exe's file and product version, and the Installed-apps entry. `scripts\build-release.ps1 -Version X.Y.Z` sets all of them. Details, tags and the release workflow: [RELEASING.md](RELEASING.md).

## Icon

The icon appears as the exe's icon, the tray icon (the `APP` icon group, loaded at runtime), the Start Menu shortcut, and the Installed-apps entry. Without resources, the tray shows Windows' generic application icon.

`assets\icon.ico` is generated from the logo `assets\resound.png` (512 px). It holds every size Windows uses at 100–200 % display scaling (16, 20, 24, 30, 32, 36, 40, 48, 60, 64, 72, 80, 96 and 256 px). With only one large image, Windows shrinks it on the fly and the icon looks soft in the Start Menu, the taskbar and the tray. The tray loads its DPI-correct small size with `LoadIconMetric`.

To replace it:

1. Replace `assets\resound.png` and run `go run ./scripts/icons`. It scales every size with a high-quality filter, sharpens small sizes slightly, and also refreshes the phone icons.
   - Small sizes can't show fine detail. For the crispest result, draw simplified versions and save them as `assets\icon-16.png`, `icon-20.png`, `icon-24.png` (optionally `icon-30.png`, `icon-32.png`), each exactly that many pixels square with a transparent background. The tool then uses them instead of scaling.
2. Regenerate the resources (the release script does this):
   ```powershell
   go-winres make --in winres\winres.json --out cmd\resound\rsrc --arch amd64
   ```
3. Rebuild the exe.
4. If Windows still shows the old icon, the icon cache is stale. Restart Explorer, or run `ie4uinit.exe -show`. Reinstalling a release refreshes the shortcut.

Paths in `winres.json` are relative to the JSON file, hence `../assets/…`. The same file holds the DPI manifest (per-monitor v2, common controls v6, `asInvoker`) and the version info.

## Panel icons

Icons are SVG files in `internal\ui\icons\`, embedded at build time. To add one from Tabler (default) or Font Awesome Free:

```powershell
.\scripts\add-icon.ps1 refresh, player-play
.\scripts\add-icon.ps1 -Set fa-solid volume-high    # -> ui.Icon("fa-solid-volume-high")
```

Commit the SVG, use it as `ui.Icon("<file name>")` (or add a constant in `internal\ui\icon.go`) and rebuild. `go test ./internal/ui` renders every embedded icon. Details and licenses: `internal\ui\icons\README.md`. The panel logo is `assets\resound.png`, embedded by `assets\assets.go`.

## Phone app

The phone remote is a separate web app in `webapp/`, hosted as a static site; nothing of it is in the exe, so changing it needs no rebuild of Resound. See `webapp/README.md`.

```powershell
go run ./cmd/webapp                          # builds webapp/dist: upload that folder to Cloudflare Pages
go run ./cmd/webapp -serve :8095             # builds and serves it on http://localhost:8095
go run ./cmd/devremote -folder <sounds>      # the PC side without audio; prints a pairing link for localhost:8095; Enter = Refresh
```

To test a local app with the real Resound, start it with `RESOUND_APP_URL=http://localhost:8095` set: its QR code and "Open the app on this PC" then use the local app (phones need the HTTPS deployment).

## Tests

```powershell
gofmt -l .               # lists unformatted files; CI fails on any
go vet ./...
go test ./...            # audio mixer/clips/cache, library, server (API, pairing, WebRTC), app files, routing helpers
go test -race ./...      # same with the race detector (needs a C compiler on Windows; optional)
```

`app`, `install` and the WASAPI code are only checked by vet and manual testing; see the checklist below. `ui` has tests for the slider and button-group logic and for icon rasterizing; painting is checked by hand.

## Commits and dependencies

**Commit messages** start with a type: `feat:` (new feature), `fix:` (bug fix), `ui:` (look and feel), `docs:`, `chore:` (build, dependencies, cleanup). They become the release notes, so write them for users: `fix: phone remote reconnects after the PC wakes up`.

**Dependencies.** The project is pure Go without cgo; keep it that way. New Go modules and front-end libraries must have a license compatible with GPL-3.0 (MIT, BSD, Apache-2.0, …) and are listed in [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md). Front-end libraries are vendored under `webapp/src/vendor/` with their license file, not loaded from a CDN.

## Testing install and uninstall locally

1. Build a release to a folder outside the repo, e.g. with `scripts\build-release.ps1`, and start it. It installs and the welcome notice appears.
2. Check: `%LOCALAPPDATA%\Programs\Resound\Resound.exe`, Start Menu "Resound", and Settings → Installed apps. Autostart is off after install; turn it on in the guided setup and check Task Manager → Startup apps.
3. Start the release exe again with a higher `-Version`: it stops the running copy, replaces it, and shows "updated".
4. Uninstall from Installed apps, or from the panel → App & startup.

**VB-Cable install** (use a VM or a PC without VB-Cable; remove it with `VBCABLE_Setup_x64.exe` → Remove Driver, then restart):
- Setup → voice step → "Install VB-Cable". Check: download progress, the UAC prompt naming VB-Audio, possibly "Install this device software?", then "Restart now".
- Click away during UAC: a corner notice appears when the install is done.
- `HKCU\…\RunOnce` → `Resound` and `%LOCALAPPDATA%\Resound\vbcable-install.json` exist. Restart: Resound starts on its own (the log's `starting ["--resume-setup"]` line), shows "Welcome back", and its panel opens on the voice step. Both entries are gone. Repeat once with the install done within a minute of starting the app: then only `RunOnce` brings it back, after the other startup apps.
- Default devices are unchanged afterwards (Windows → Sound). If Windows had switched them to CABLE, the log shows `vbcable: Windows had made the cable the default; restored …`.
- Quit and start only the app before restarting Windows: it shows "Restart needed" again.
- Decline UAC, or disconnect the network: an error appears next to "Install manually".
- Signature check against a real setup: `$env:VBCABLE_SETUP="<path>\VBCABLE_Setup_x64.exe"; go test -run VerifySigner -v ./internal/vbcable`. Live download (no install): `$env:VBCABLE_DOWNLOAD="1"; go test -run Download -v ./internal/vbcable`.

Uninstalling deletes `%LOCALAPPDATA%\Resound`, which is the same config and pairing data your dev builds use.

**Manual reset**, if needed:
- Delete `%LOCALAPPDATA%\Resound` and `%LOCALAPPDATA%\Programs\Resound`.
- Delete the registry keys `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\Resound`, `HKCU\…\CurrentVersion\Run` → `Resound` and `HKCU\…\CurrentVersion\RunOnce` → `Resound`.
- Delete the Start Menu `Resound.lnk`.
- Optionally, as admin: `netsh advfirewall firewall delete rule name=Resound`.

## Testing updates locally

The in-app update ([ARCHITECTURE.md](ARCHITECTURE.md#install-update-uninstall)) normally asks GitHub. `cmd/devupdate` stands in for GitHub's release API, so the whole flow can be tested without publishing anything:

```powershell
.\scripts\build-release.ps1 -Version 0.7.0     # the "old" version: Desktop\Resound-0.7.0
.\scripts\build-release.ps1 -Version 0.9.9     # the "new" version: Desktop\Resound-0.9.9

# Terminal 1: offer 0.9.9 as the latest release
go run ./cmd/devupdate -exe $HOME\Desktop\Resound-0.9.9\Resound.exe -version 0.9.9 -slow 5s

# Terminal 2: install 0.7.0 with the check pointed at devupdate
$env:RESOUND_UPDATE_API = "http://127.0.0.1:8090"
& $HOME\Desktop\Resound-0.7.0\Resound.exe
```

Check, within about 10 s: the home page's header shows `v0.7.0` and a dot with "Update", which opens App & startup with "New version v0.9.9". "Update now" shows the download progress; then Resound quits and restarts as 0.9.9 with "Resound is updated", and `debug.log` shows the `update: …` lines. Variants: `-bad-checksum` (refused; the old version keeps running), `-status 404` ("Up to date": no release yet), `-status 403` (the rate-limit error). Without `RESOUND_UPDATE_API`, the app asks the real GitHub.

`go test ./internal/input` covers the hotkey matching. The real keyboard hook has a live test that injects F24 (a key no keyboard has) into the desktop, so it only runs on request: `$env:RESOUND_HOOK_TEST="1"; go test -run WatcherLive -v ./internal/input`.

It also checks what each push-to-talk preset sends (virtual key and scan code; Pause once went out as Right Ctrl). A live test presses every preset into a window of its own and checks what window messages and raw input see, with Num Lock on and off: `$env:RESOUND_KEYS_TEST="1"; go test -run PresetsLive -v ./internal/input`. Keys remapped on that PC (PowerToys Keyboard Manager, mouse software) fail there.

The panel's position rules (docked, kept inside a work area) are unit-tested in `go test ./internal/ui`. A live test shows the real panel in the test process (not the running Resound, whose config it leaves alone) and drags it past every screen edge, onto every screen, back with Esc and to a screen that no longer exists: `$env:RESOUND_PANEL_LIVE="1"; go test -run PanelMoveLive -v ./internal/ui`. By hand, with the dev build: drag the tab's handle onto each screen and against each edge, switch between short and long pages near the bottom, restart (it opens where it was left), change the scale or resolution in Windows' display settings while it is open, unplug a screen it is on (it docks at the taskbar), and reset it with a double-click on the handle and from the tray icon's right-click menu. The mouse's back button over the panel goes back a page and never closes it.

`go test ./internal/editbridge` covers the edit page's server: serving the file, clips, the reload grace, Done (`done`, and only the page's own window titles) and Stop editing (`state`). To try the editor without the panel: `go run ./cmd/webapp -serve :8095`, then `go run ./cmd/devedit -file <video or sound> -out <folder>` (`-replace` to edit a sound in place) and open the address it prints with `msedge --app=<address>`, as the panel does. Enter in devedit's console is "Stop editing"; Done, a saved sound and Stop editing close the app window.

`go test ./internal/update` covers the version comparison, the checksum and the allowed hosts against a fake GitHub.

`go test ./internal/importer` covers finding the sound on a page (a trimmed Myinstants page in `testdata/`), names, the address guard and downloads against a local server. A live test downloads real sounds from Myinstants and Sound Buttons World with the real client and decoder (and checks that their list pages are refused): `$env:RESOUND_LIVE="1"; go test -run Live -v ./internal/importer`.

To try the import in the panel: quit the installed Resound, run the dev build (`go run ./cmd/resound`), open the Sound library page (or the home page's "Folders" card), click "Myinstants" (or "Sound Buttons World"), and drag a sound's name onto a folder. Check: the folder lights up while dragging; its icon turns green, its count reads "Downloading…", and a lighter band joins it with the card that follows: the card with the name field (the page's name), Discard, Preview and Save, where Enter renames and Esc keeps it; afterwards the folder reads "Added “…”" for 3 seconds. Dropping the same sound again warns that it is already in the library and saves it as "Name (2)". A search page's address gives "No sound file found"; a link dropped outside the folders asks to drop it onto one.

## Debugging

- **Log:** `%LOCALAPPDATA%\Resound\debug.log` (panel → Settings → Diagnostics → Open log).
  - `startup: …` lines mark each startup phase.
  - Fatal Go errors and panics, with stack traces, are written there too (`debug.SetCrashOutput`).
  - Crashes Go can't report (a native crash on a Windows thread, a hang, Task Manager) show at the next start as `previous run: …` lines, with what Windows' Application event log recorded for that process (faulting module, exception code).
  - The elevated helpers (`routing helper: …`, `firewall helper: …`) log to the same file.

| Symptom | Look at |
|---|---|
| Exits right after start | End of `debug.log` (crash trace), or start the debug build from a terminal |
| No sound / output "Error" | `… output open` lines, Settings → Diagnostics; "Restart outputs" after unplugging |
| Others don't hear the voice | Settings → Audio & voice chat: a broken mic pipe ("Not routed"); `input: … listen=… target=…` lines |
| Phone can't connect | `app: reachable at …` and `app: device … connected` lines; Remote App → "Allow phones through the firewall"; same Wi-Fi, not a guest network |
| Game ignores push-to-talk | `push-to-talk down/up` lines; games running as admin don't see input from normal apps |
| Pause/stop key does nothing | `hotkeys: watching …` at start, `hotkey: …` lines when pressed; inside a game running as admin, Resound sees no keys |
| UDP port 8080 busy | Remote App page shows the error; change `serverPort` in `config.json` |

## Release checklist

Moved to [RELEASING.md](RELEASING.md#release-checklist).
