# Resound architecture

State as of v0.6.0-dev (library index, streaming playback). For building and icons, see [BUILDING.md](BUILDING.md); for releases, [RELEASING.md](RELEASING.md).

## What it is

Resound is a Windows tray app that plays sounds into a virtual audio cable (so others hear them in games or voice chat) and to your own speakers. It is controlled from a phone browser over the local network. It replaces the Soundux desktop app (C++/WebView2) with one Go executable: no cgo and no UI framework, ~9k lines.

```
 phone browser ──HTTP──▶ server ──Player──▶ app (UI thread) ──▶ audio.Engine ─┬─▶ "You hear"   WASAPI stream ─▶ speakers
 (embedded web app)        │                  │                                └─▶ "Others hear" WASAPI stream ─▶ CABLE Input
                         library           routing / input                                      │
                                           (mic mute, PTT)        mic ─"Listen to this device"─▶┘ CABLE Output ─▶ game / Discord
 tray icon ─▶ popup panel (ui) ───────────────▲
```

## Repository layout

```
cmd/resound/     entry point
cmd/devremote/     phone remote with the real library but no audio, for front-end work on any OS
internal/app/      app state, panel pages, playback holds, push-to-talk, server bridge
internal/audio/    WASAPI outputs, mixer, progressive decoding
internal/wincom/   Core Audio COM plumbing and go-wca fixes
internal/routing/  VB-Cable detection, mic mute, "Listen to this device" (elevated helper)
internal/vbcable/  automatic VB-Cable install (download, signature check, silent setup, resume after restart)
internal/input/    push-to-talk via SendInput, key presets
internal/library/  sound folders → tabs/sounds with stable IDs
internal/server/   phone API, PIN/QR auth, embedded web app (web/)
internal/install/  install/update/uninstall, autostart, firewall
internal/backup/   settings backups (zip), restore, backup index
internal/elevate/  one-shot elevated helper runs (UAC)
internal/ui/       tray, panel, rows, dropdown, tooltips, dialogs (Win32 + GDI)
internal/config/   config.json        internal/logx/  log + crash output
internal/ui/icons/ embedded SVG icons (scripts/add-icon.ps1)
assets/            app icon (exe/tray) and the panel logo (embedded)
winres/            icon/manifest/version resources (go-winres)
scripts/           build-release.ps1, add-icon.ps1, icons/ (icon generator)
docs/              BUILDING.md, RELEASING.md, ARCHITECTURE.md
.github/           CI and release workflows, Dependabot
```

## Processes and entry points

One executable, `cmd/resound` → `app.Run()`. The first argument selects a role:

| Invocation | Role | Elevated |
|---|---|---|
| *(none)*, `--autostart`, `--welcome=install\|update` | Tray app. A non-portable exe outside the install folder installs itself first | no |
| `--resume-setup` | Tray app, started again after the restart a VB-Cable install needs, by Windows' restart-apps registration or `RunOnce` (what to resume comes from `vbcable-install.json`) | no |
| `--apply-routing --input=… --target=… --enable=…` | Writes "Listen to this device" on a mic, verifies, exits | yes (UAC) |
| `--allow-firewall` | Replaces the exe's firewall rules with one allow rule, exits | yes (UAC) |
| `--uninstall` | Confirm dialog, then removes app, registrations and data | no |
| `--restore=<zip> --wait-for=<pid>` | Tray app started by "Restore": waits for the old instance to exit, saves a safety backup, restores the zip, then runs normally | no |

Helper roles run before the single-instance mutex (`Local\Resound.SingleInstance`). A second tray start sends `wmShowPanel` to the running instance, which opens its panel. The installer and uninstaller send `wmQuitApp`, which makes the running instance quit.

## Packages

| Package | Responsibility |
|---|---|
| `app` | Wiring and all app state. Panel pages (`pages.go`, `wizard.go`, `diagnostics.go`, `system.go`), playback holds (`playback.go`), push-to-talk (`ptt.go`), devices and routing actions (`devices.go`), server bridge (`remote.go`) |
| `audio` | WASAPI outputs (`output_windows.go`), mixer (`mixer.go`), streaming sources and ring buffer (`source.go`), streaming decoder (`decode.go`), header-only length probe (`probe.go`), engine API (`engine_windows.go`), mix-format parsing (`format_windows.go`) |
| `wincom` | Core Audio COM plumbing: COM threads, endpoint enumeration/lookup, and fixes for go-wca bugs |
| `routing` | VB-Cable detection, mic mute, the undocumented "Listen" properties, the elevated apply command |
| `vbcable` | Automatic VB-Cable install: download, signature check, silent elevated setup, default-device snapshot/restore, the pending-install state across the restart |
| `input` | `SendInput` key and mouse presses with scan codes, key names, quick-pick presets |
| `library` | Sound index (`library.json`): folders → tabs → sounds with stable IDs, rename/move matching (`reconcile.go`), per-sound settings and phone layouts, background indexer (`indexer.go`) |
| `server` | HTTP API (Soundux Remote compatible), auth (PIN, sessions, QR pairing), LAN addresses, embedded `web/` |
| `install` | Drop-in install/update, Installed-apps entry, Start Menu shortcut, autostart, uninstall, firewall helper |
| `elevate` | Runs this exe with `runas` and returns its exit code |
| `ui` | Tray icon, popup panel, rows, dropdown, tooltips, dialogs, file/folder pickers (Win32 + GDI); embedded SVG icons and images (`icon.go`, `icons/`) |
| `assets` | The logo PNG embedded for the panel header (`assets.go`) |
| `backup` | Backup zips of `config.json`, `library.json` and `remote.json` with a manifest; validation, clean restore, backup index |
| `config`, `logx` | `config.json`; log file plus crash output |

Platform-specific files use the `_windows.go` suffix. `mixer.go`, `source.go`, `decode.go`, `probe.go`, `library` and `server` are portable and unit-tested on any OS. `cmd/devremote` serves the phone remote with the real library and indexer but no audio output, for front-end work on any OS (`go run ./cmd/devremote -folder <sounds>`).

## Threading model

- **UI thread.** The goroutine that calls `ui.NewTray` is locked to its OS thread and runs the message loop. All `App` state lives here.
  - Other goroutines reach it with `Tray.Post(fn)`, which queues `fn` and posts `wmInvoke`.
  - Server handlers use `App.onUI(fn)`: Post, then wait up to 3 s.
- **Audio threads.** Each output has its own goroutine, locked to an OS thread with COM (MTA) and MMCSS "Pro Audio" priority. It runs an event-driven render loop and owns all of that stream's COM objects. The mixer's lock only guards its voice list.
- **COM calls from the app** (device lists, mute, routing checks) go through `wincom.Run`: a fresh locked MTA thread per call. The UI thread is apartment-threaded (STA) only for shell dialogs.
- **Server.** Standard `net/http` goroutines. Read-only queries go straight to the thread-safe `library` and `engine`. Anything that changes app state goes through `onUI`. Decoding happens on the HTTP goroutine, never on the UI thread.
- **Engine events** (first buffer, finished, output failed) go into a 256-slot channel without blocking the render thread. `pumpEvents` forwards them to the UI thread.
- **Dialogs** (`ui.TryConfirm`) run on their own locked thread with their own message loop. They work in processes without a tray.
- **Elevated helpers** are separate processes started through `elevate.Run`, waited on from a goroutine, with results posted back.

## Audio pipeline

- **Outputs.** There are two roles: `monitor` ("You hear", empty ID = system default) and `cable` ("Others hear", off when not set).
  - Each role is one long-lived shared-mode WASAPI stream with a 20 ms requested buffer (22 ms measured), opened in about 10 ms.
  - The stream writes the device's mix format: 32-bit float or 16/24/32-bit integer, 1–8 channels. Stereo goes to front left/right, mono gets (L+R)/2.
- **Mixer.** Sums voices into stereo float32 and hard-clips.
  - A voice has a gain that either scales the role volume or, for per-sound custom volume, replaces it.
  - The same voice ID is queued on both mixers, so the two copies start in the same device period. Pause and stop address both copies by ID.
- **Streaming playback.** Every play decodes the original file while it plays (`gopxl/beep`: mp3, wav, flac, ogg, resampled to the output rate). Nothing decoded is kept in memory or on disk after a sound ends.
  - One producer goroutine (one decoder) per sound and distinct output sample rate fills a 500 ms single-producer/single-consumer ring buffer per output. With both outputs at 48 kHz, a sound is decoded once. The mixer drains its ring on the render thread without locks, blocking or disk access; a producer that falls behind causes silence, not an error.
  - Memory per playing sound is fixed (~190 KB per output at 48 kHz) whatever the file's length.
  - `PrepareSound` returns once 100 ms are decoded, before the mic is muted. Measured on Linux from the OS file cache: ~1.5 ms for a 10-minute WAV, ~3 ms for a 60-minute MP3.
  - MP3 files are opened without `Seek`: given a seekable file, the decoder parses every frame of the file before the first sample (114 ms for a 60-minute file even from the OS cache). The length comes from the index instead.
  - When a sound was decoded to its end, its exact length is stored in the index (`exactLength`).
  - Stop, finish and output shutdown (`Mixer.Drain`) close sources; a producer stops and closes its file when none of its sources is left.
- **Test chime** (`ChimeSource`): three rising FM blips with a short pitch bend, panned left, right, centre; 0.45 s, generated in memory.
- **Measured on the dev PC:** 0.5–10 ms from play to the first device buffer, plus the 22 ms buffer.

## Playback holds: mic mute and push-to-talk

While sounds play for others (any non-preview play), `holdPlayback` does two things, each if enabled:
- mutes the real mic (`IAudioEndpointVolume`), never touching a mic the user muted themselves;
- presses push-to-talk.

`releasePlayback` runs when the last voice finishes, on stop-all and on output failure.

Push-to-talk uses **reasons** (`playback`, `talk`). The keys go down when the first reason appears and up when the last one goes away. Talk-through adds `talk` before pausing the sounds, so the key never bounces during the hand-over.

Pause and talk-through pause the active voices and release the mic and PTT. Resuming holds them again, but only if the voices still exist.

## Routing (Windows side of VB-Cable)

- **The model.** The game's mic is "CABLE Output". Resound plays into "CABLE Input". The real mic reaches the cable through Windows' "Listen to this device" with "Playback through" = CABLE Input.
- **Undocumented properties.** "Listen" is stored in two properties of the capture endpoint, `{24dbb0fc-9311-4b3d-9cf0-18ff155639d4}`, pid 1 (VT_BOOL) and pid 0 (VT_LPWSTR, a render endpoint ID). Reading them needs no rights; writing needs admin, so the elevated `--apply-routing` helper writes and verifies them.
- **Detection.** VB-Cable is detected by name: render `CABLE Input…`, capture `CABLE Output…`. The 16-channel and A/B variants are ignored. The automatic mic choice is the default recording device unless it is CABLE Output.

## Installing VB-Cable for the user

"Install VB-Cable" (setup's voice step and Settings → Voice chat; `app/cable.go`, package `vbcable`):

1. **Snapshot** the default playback and recording devices (console and communications roles).
2. **Download** `VBCABLE_Driver_Pack45.zip` from `download.vb-audio.com` over HTTPS (redirects only within vb-audio.com, size-limited) into `%LOCALAPPDATA%\Resound\vbcable\`, and unpack it (flat entries only).
3. **Verify** `VBCABLE_Setup_x64.exe`: a valid Authenticode signature (`WinVerifyTrust`) whose primary signer is VB-Audio's author ("BUREL VINCENT …"). Anything else falls back to the manual download.
4. **Record** the pending install (`vbcable-install.json`: start and boot time, the page to return to, the default-device snapshot) *before* the setup runs.
5. **Run** the setup elevated with `-i -h` (silent install). The UAC prompt names VB-Audio as the verified publisher. Windows may also ask "Install this device software?"; that prompt cannot and should not be suppressed. The exit code isn't documented, so success is judged by the devices appearing (polled for 20 s).
6. **Restore defaults.** Windows often makes CABLE Input/Output the default devices when they appear: the user hears nothing, and "System default" outputs and the automatic mic would follow. Each default that moved *to the cable* goes back to the snapshot through `IPolicyConfig::SetDefaultEndpoint`; defaults the user chose are left alone.
7. **Restart.** The panel offers "Restart now": required while the devices are missing, recommended by VB-Audio once they appear ("Not now" then drops the pending state). Before restarting, playback stops, the mic and keys are released and everything is saved. Two ways bring the app back, both with `--resume-setup`:
   - **Restart-apps registration** (fast): `RegisterApplicationRestart`, then `ExitWindowsEx(EWX_REBOOT|EWX_RESTARTAPPS)`. Windows starts the app again at sign-in (expected to be quicker than `RunOnce`; not yet measured), but only if it had been running for at least 60 seconds.
   - **`HKCU\…\RunOnce`** (fallback, registered as soon as the setup finishes): always works, but Windows runs it only after every `Run` entry, one after another, waiting up to ~30 s for each. On a PC with many startup apps that takes minutes (3.5 minutes were measured on a dev PC; the Shell-Core event log, events 9705–9708, shows the order).
8. **Resume.** At start-up (before the outputs open), a pending install from an earlier boot means Windows restarted: defaults are checked again, the state file and `RunOnce` entry are removed (so a fast start isn't followed by a second one minutes later), the panel page is set (the setup at its step, or Voice chat), and a corner notice offers to continue. Without a restart in between, the app only shows "Restart needed" again and re-arms `RunOnce`.

Failures (no network, declined UAC, bad signature) leave the manual "Install manually" path, which works as before. Every start logs its arguments, so `debug.log` shows how the app was started. The guided setup's step is saved in `config.json` (`setupStep`), so any restart continues where the user left it.

**Licensing.** VB-Cable is donationware. VB-Audio's licensing page allows installing the unmodified package from another application, silently, as long as end users can see that it is VB-Audio's product and can donate (mentioning www.vb-cable.com and that it is donationware). The voice-chat pages therefore show a credit with links to vb-cable.com and the donation page. The pack is downloaded from VB-Audio, never bundled or modified. Professional deployments where users can't donate need purchased licenses, and VB-Cable A+B/C+D may never be bundled. The pack's own readme asks for the author's agreement before integrating it into another installer; see Known issues.

## Library and IDs

- Each configured folder becomes a tab, plus one tab per direct subfolder that contains sounds (as in Soundux). There is no deeper recursion, and sounds are listed newest first.
- **Stable IDs.** A sound or tab gets its ID when first seen and keeps it. New IDs are the 53-bit FNV-1a hash of the lower-cased path (as before, so phones from earlier versions keep working); they are exact in JavaScript.
- **Matching on scan** (`reconcile.go`), cheapest first:
  1. same path: unchanged, or edited in place (size or time differs: fingerprint and length cleared, read again);
  2. NTFS file ID (volume serial + file index) of a record whose path vanished: renamed or moved on the same drive, without reading the file;
  3. fingerprint of such a record, only tried when one has the same size: moved across drives;
  4. otherwise a new sound. Copies stay separate sounds, because only vanished records can be claimed.
  - A folder that vanished hands its tab ID to the new folder that at least half of its sounds moved to (a renamed folder keeps its phone layout).
- **Tombstones.** Records of missing files and folders are kept for 30 days (`missingSince`), so removing and re-adding a folder, or moving a file out and back, restores its settings.
- **Index file.** `library.json` holds the records, settings (favorite, volumes, phone color and emoji) and phone layouts (mode and custom orders by sound ID). It is written atomically by a single debounced saver; an unreadable file is moved aside, never overwritten. `sounds.json` from earlier versions is migrated on first start and renamed to `sounds.json.migrated`.
- **Indexer.** One background worker reads new and changed files, one at a time, without decoding audio:
  - a **fingerprint**: SHA-256 (first 128 bits) over the size, the first 64 KB and the last 64 KB, so at most 128 KB per file;
  - the **length** from the headers (`audio.ProbeDuration`): WAV/FLAC/OGG headers; for MP3 the Xing/Info or VBRI frame count, or the bit rate for constant-bit-rate files (an estimate for VBR files without a VBR header, corrected after the first full play).
  - Progress shows in the panel; sounds are playable before they are indexed.
- `GET /api/tabs` and `GET /api/library` rescan when the last scan is more than 5 s old.

## Server and authentication

- **Routes:** all 14 Soundux Remote endpoints (`/api/tabs`, `/api/sounds/{id}/play|preview|favorite|volume[/reset]`, `/api/sounds/progress|settings|stop`, `/api/playback/toggle`, `/api/talkthrough/start|stop`, `/api/status`) plus `/api/auth/login|check|logout` and `/pair?code=`.
- **Resound additions:**
  - `GET /api/library`: tabs (with sound IDs in order), sounds (with favorite, custom-volume flag, color, emoji, duration) and layouts in one response. It carries an `ETag` (`"<server start nonce>-<revision>"`) and answers `If-None-Match` with 304, so phones can poll it cheaply.
  - `POST /api/sounds/{id}/appearance` `{color, emoji}`, `POST /api/layouts/{tabId|favorites}` `{mode, orders}`: phone card looks and layouts, stored on the PC and shared by all phones. Colors and emoji are validated because the phone puts them into class names and CSS strings.
  - `POST /api/library/import` `{overwrite, sounds, layouts}` accepts sound IDs or file paths, so settings from earlier phone versions (path-keyed) and exported files can be adopted. Without `overwrite`, only empty values are filled.
  - `POST /api/library/reset-appearance` clears all colors, emoji and layouts.
- **Phone persistence (v3).** Device preferences (last tab, fullscreen, button position) stay in `localStorage`; everything about sounds and tabs lives on the PC, keyed by ID, with an in-memory mirror for synchronous reads. On first connect after the update, a phone sends its old path-keyed settings to the PC once.
- **Response shapes** match the C++ server, including the volume slider model (−50…+50 scales both default volumes from 0× to 2×).
- **Addresses.** `LocalAddresses` ranks private addresses of physical adapters first and labels each adapter ("Ethernet", "Wi-Fi", "WSL", "Hyper-V", "VPN", …). Virtual adapters and public addresses are marked `Other`; the panel folds them behind "Show other addresses".
- **Remote on this PC.** The home page's tile button and the Phone remote page open `http://127.0.0.1:<port>/pair?code=…` in Edge's app mode (a separate phone-sized window), or the default browser without Edge. The one-time pairing code signs that browser in and replaces its older session, so it counts as one paired device.
- **Auth.**
  - The 6-digit PIN persists until reset.
  - A login or QR pairing creates a 32-byte token in an HttpOnly `SameSite=Lax` cookie. Sessions expire after 180 days of inactivity, and only SHA-256 hashes are stored (`remote.json`).
  - Five wrong PINs from one address cause a 1-minute lockout.
  - QR codes are single-use and valid for 10 minutes. Re-pairing replaces the phone's old session.
  - There are no CORS headers (same-origin only), and the PIN and tokens are never logged.
- **Web app.** `internal/server/web/` is embedded with `go:embed all:web`. Without `index.html`, a placeholder page is served.

## UI framework

- **Panel.** The panel is one borderless, DPI-aware popup. Pages are `[]ui.Row` lists **rebuilt from state on every change** (`App.render`). Rows are immutable values implementing `height/segments/paint/click/keepsOpen`.
  - Optional interfaces: `opener` (opens something inside the popup), `tipper` (provides a full-text tooltip), `measurer` (height follows the content, e.g. wrapped text), `segmentKeeper` (per-segment `keepsOpen`), and `dragger` (a segment follows the mouse while the button is held).
- **Size and shadow.** The panel is 340 logical pixels wide and at least 560 high (`popupMinL`), so switching pages doesn't make it jump. Short pages leave the rest empty, or give it to their first `Flex` row, which keeps the footer after it (Settings / Quit, Continue) at the bottom. Taller pages grow the panel up to the work area and then scroll. The drop shadow is a click-through layered window placed directly beneath the panel (`shadow_windows.go`). It shows a blurred, lowered, translucent black rounded rectangle computed in Go (`shadow.go`, three box blurs), so it is equally strong on Windows 10 and 11 and whether or not the panel has focus. `CS_DROPSHADOW` and DWM's frame shadow were too faint.
- **Row types:** Brand (logo, letter-spaced title, status), Title, Header (back button), WizardHeader (back button and progress dots), Section, Text, Paragraph (word-wrapped), Flex (fills a short page), DropZone (dashed drop field, clickable), Button / ClosingButton, Buttons (side by side, each with an optional icon and optionally closing), Nav, NavTool (Nav with an icon button on its left), Radio, Toggle, Slider, Stepper, Step, Item, Dropdown, QRCode, Separator, Spacer. Nav tiles are as wide as buttons.
- **Icons and images.** SVG icons in `internal/ui/icons/` (Tabler, Font Awesome; added with `scripts/add-icon.ps1`) are embedded and rasterized in pure Go (oksvg/rasterx) as anti-aliased alpha masks at the exact device-pixel size. They are tinted with the row's color and drawn with `AlphaBlend` from a premultiplied 32-bit DIB section, cached per name, size and color. Images (`ui.NewImage`, the logo) are scaled with Catmull-Rom and drawn the same way. GDI's own shapes have no anti-aliasing; text does (ClearType). See `internal/ui/icons/README.md`.
- **Slider.** − and + move to the previous/next multiple of the step; clicking or dragging the track sets the value. The popup captures the mouse while dragging and re-finds the row by id after each re-render. `onChange(value, done)` fires on every move with `done=false` and once with `done=true` on release or a button click, so the app applies volumes live but saves `config.json` once.
- **Pages.** Three layers, each built from shared row builders (`folderRows`, `libraryActions`, `monitorDropdown`, `volumeSliders`, `cableRows`, `pairRows`, …):
  - **Guided setup** (`wizard.go`): shown until `config.SetupComplete`. Welcome, then five skippable steps: sounds, speakers (output, volumes, test chime), voice chat (VB-Cable; one button selects CABLE Input and routes the mic), push-to-talk key, phone pairing. Configs from before the setup existed (folders or a cable set) count as complete. Settings → "Run the guided setup again" restarts it.
  - **Home**: everyday use. Logo and status, problems (failed output, voice chat incomplete), the library tile with a refresh button and what the refresh found (folders are added on the library page), the output device and volume sliders, test chime / stop, the phone remote, Settings and Quit.
  - **Settings**: Voice chat (cable, mic), Push-to-talk, Audio outputs, Sound library, Phone remote, App & startup, Backup & restore (one-click backup, Save as…, the backup list, restore from any file; also offered on the setup's welcome page), and Diagnostics (formats, buffers, latency test, index, server, log/data/program folders). A detail page's back button returns to the page that opened it.
- **Restoring a backup** restarts the app: the running instance would rewrite the config and sound index when it exits, so it starts a new instance with `--restore` and quits. The new instance waits for it, saves the current state as a `…-before-restore.zip`, and replaces the data files. A clean restore also removes data files the backup doesn't contain. Backups are checked first: they need the manifest, may contain only the known files as valid JSON, and are size-limited.
- **Drag and drop.** The Sound library page and the setup's sounds step pin the panel (`Popup.SetPinned`): it stays open when another window gets the focus, so folders can be dragged in from Explorer, and only then accepts drops (`DragAcceptFiles`, `WM_DROPFILES`). A dropped folder becomes a library folder, and a dropped sound file adds its folder. Folders that are already in the library, or are a direct subfolder of one (already a tab), are skipped. The folder picker is owned by the panel, so it opens above it. Leaving the page un-pins the panel; clicking the tray icon closes it.
- **Dialogs from the panel.** Folder and file pickers run from closing buttons; afterwards `reopenPanel` shows the panel again with the result. UAC prompts are not followed by a reopen: activating the popup from the background could fail and leave it open without focus.
- **Dropdown.** The list is an overlay inside the popup, not its own window, so focus never moves and the panel stays open.
  - It opens below or above the field depending on space, shows up to 8 items, and scrolls with the wheel or keys.
  - Its `id` re-attaches it to its row after re-renders.
- **Tooltips** appear after 450 ms, only for cut-off text, word-wrapped across the panel width.
- **Key capture.** While the panel has focus, `OnKey` receives keys and mouse buttons 3–5, with left and right modifiers kept separate.
- **Dialog** (`ui.Confirm`, `TryConfirm`): ported from the Skira Seed Helper. Input is ignored for the first 400 ms, Cancel is the default, and corner/passive mode shows the hidden-icons hint.

## Install, update, uninstall

- **Install folder:** `%LOCALAPPDATA%\Programs\Resound\Resound.exe`.
- **Registry (HKCU):**
  - `…\Uninstall\Resound` — the Installed-apps entry;
  - `…\Run\Resound` — autostart, only once the user turns it on (guided setup or App & startup); installing doesn't.
  - `…\RunOnce\Resound` — `--resume-setup`, only between a VB-Cable install and the next sign-in.
- **Start Menu shortcut:** `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Resound.lnk`, created through WScript.Shell.
- **Update detection:** a version string in `installed_version.txt`. A different string means an update.
- **Replacing a running exe:** the old one is renamed to `.old`, and a later start deletes it.
- **Uninstall:** the exe deletes itself through a delayed hidden `cmd.exe`.
- **Portable builds never install.** A build is portable when a `go.mod` sits next to the exe or one folder up, a `portable` file sits next to it, or `RESOUND_PORTABLE=1` is set.

## Data files (`%LOCALAPPDATA%\Resound`)

| File | Content |
|---|---|
| `config.json` | Devices, volumes, mic, PTT keys and options, folders, server port |
| `library.json` | Sound index: records with stable IDs, fingerprints, lengths, settings, phone layouts, tombstones |
| `sounds.json.migrated` | Per-sound settings of earlier versions, kept after migration |
| `remote.json` | PIN and session token hashes |
| `installed_version.txt` | Version of the installed copy |
| `vbcable-install.json` | A VB-Cable install waiting for the Windows restart (removed after it) |
| `vbcable\` | The downloaded VB-Cable pack, only while its setup runs |
| `backups\` | Backup zips (`Resound-backup-<date>_<time>.zip`) and `index.json`, which also lists backups saved elsewhere |
| `debug.log` | Log and crash output, reset when over 2 MB at start |

## Key technical choices

| Choice | Why |
|---|---|
| Go without cgo | Plain `go build`. The same workflow as the Seed Helper, one small exe |
| Own WASAPI render loop (go-wca) | `oto` can't pick a device or open two streams; `malgo` needs cgo. Long-lived streams also beat Soundux's device-per-play |
| Decode with beep while playing, from the original file | Pure Go decoders; no duplicate of the library on disk, no warm-up, edits are always heard. Decoding is far faster than real time, so starts take milliseconds |
| One decoder per sample rate, fanned out to the outputs | Two outputs don't double the CPU work |
| Small ring per output | Memory doesn't grow with sound length or library size; Windows' file cache keeps recently played files fast for free |
| Fingerprint of head and tail, header probes | Indexing reads at most a few hundred KB per file, even for large libraries |
| Stable IDs for identity | Renames and moves keep settings |
| No decoded-audio cache | Tried in 0.6 development: ~11 MB per minute of audio and seconds of decoding per music file, for a start-up gain of a few milliseconds |
| Win32 + GDI panel instead of WebView | No WebView2 runtime or NuGet; tiny; full control over tray behaviour |
| Rows rebuilt per render | No widget state to keep in sync; pages and wizards are plain functions |
| Elevated one-shot helpers | The app never runs as admin; only the two admin writes do |
| IDs assigned once (initially the path hash) | Stable across restarts, removed tabs, renames and moves, unlike Soundux's list indexes |
| JSON index instead of a database | Thousands of sounds are a few hundred KB; loads in milliseconds, readable when debugging, no dependency |
| Phone controls via existing web UI | Reuses the Soundux Remote front end unchanged |

**go-wca workarounds** (all in `wincom` or `routing`; avoid the go-wca versions of these calls):
- `IMMDevice.Activate` passes a pointer where the class-context flag belongs. Use `wincom.Activate`.
- `IMMDeviceEnumerator.GetDevice` is an empty stub. Use `wincom.Find`.
- `IAudioEndpointVolume.GetMute` writes a 4-byte BOOL into a 1-byte Go bool, which corrupts memory. Use `wincom.GetMute`.
- `IPropertyStore.SetValue` and `Commit` are empty stubs. `routing` calls them directly.

## Known issues and gaps

Ordered by impact on reliability. None blocks normal use. Fixed in 0.6: concurrent writes to `sounds.json`, edited files playing old audio, long sounds held fully in memory, decoded clips accumulating in memory.

1. **The mic or push-to-talk can stay held.**
   - (a) If the process is killed during playback, the mic stays muted and a held key stays down until it is physically pressed. Sign-out, restart and shutdown are handled (`WM_ENDSESSION`); a crash or a Task Manager kill is not. Fix: persist "muted by Resound" and undo it on start.
   - (b) Engine events are dropped when their channel is full. A lost `EventFinished` keeps `remaining` non-empty, so the mic and PTT stay held. Fix: while holding, regularly compare `remaining` with `engine.Voices()`.
2. **Device changes are not followed.** There is no `IMMNotificationClient`: "System default" doesn't follow Windows' default device, and an unplugged device stays failed until "Restart outputs". Fix: register for endpoint notifications and reopen affected outputs.
3. **Updates if the old instance won't quit.** If the running copy doesn't exit within 5 s, the new copy starts, finds the single-instance mutex, and exits, so the old version keeps running. Fix: wait for the old process (not just its window), or show an error.
4. **Uninstall leftovers.** The firewall rule (deleting it needs admin) and the "Listen to this device" setting remain. Fix: offer an elevated cleanup, and record what Resound changed.
5. **Version info.** `winres.json` and `app.Version` hold development defaults. Release builds must override both; `scripts/build-release.ps1` does (and the release workflow uses it).
6. **A broken `config.json` resets silently** to defaults and is overwritten on the next save. Fix: back up the broken file and log it.
7. **Output failure releases holds for both outputs.** `EventOutputFailed` clears all held voices, releasing the mic and PTT even if the other output is still playing.
8. **Plain HTTP on the LAN.** Session cookies travel unencrypted; acceptable on a home network. HTTPS would also allow the browser's Wake Lock and service workers.
9. **Elevation with a different admin account.** When a standard user approves UAC with another account, the helper's log goes to that account's `%LOCALAPPDATA%`.
10. **Front end** (planned): talk-through button bounces, 250 ms polling instead of live updates, and two remaining CDN dependencies: the Material Symbols font (Google Fonts) and the emoji picker with its emoji data (jsDelivr). NoSleep and SortableJS are vendored in `web/vendor/`.
11. **No live file watching.** Changes on disk are picked up by the 5 s rescan on phone requests, the panel's "Rescan", and the check on every play. Fix if needed: `ReadDirectoryChangesW`.
12. **ReFS and network drives** may not provide stable file IDs; renames there are matched by fingerprint instead, which needs the file to have been indexed before it moved.
13. **VB-Cable's silent install is not officially documented.** `-i -h` is the switch named on VB-Audio's forum; should a future setup ignore it, its dialogs show (it is started visibly for that reason). The pack's readme asks for the author's agreement before integrating it into another installer, while the newer licensing page allows it under the donationware conditions. Get a written confirmation from VB-Audio before a public release.
14. **Untested packages.** `app`, `install` and the `_windows.go` audio paths have no automated tests, and `ui` only for the slider and button-group logic; they are checked by vet and manual testing.
