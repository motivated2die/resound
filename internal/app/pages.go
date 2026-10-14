package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/skip2/go-qrcode"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

const vbCableURL = "https://vb-audio.com/Cable/"

// The panel has three layers: the guided setup on first start (wizard.go),
// the home page for everyday use, and Settings with the detail pages and
// Diagnostics (diagnostics.go). Row builders below are shared between them.

func (a *App) rows() []ui.Row {
	switch a.page {
	case pageWizard:
		return a.wizardPage()
	case pageSettings:
		return a.settingsPage()
	case pageAudio:
		return a.audioPage()
	case pageKeys:
		return a.keysPage()
	case pageLibrary:
		return a.libraryPage()
	case pageRemote:
		return a.remotePage()
	case pageApp:
		return a.appPage()
	case pageBackup:
		return a.backupPage()
	case pageDiagnostics:
		return a.diagnosticsPage()
	}
	return a.homePage()
}

// openPage opens a detail page; its back button returns to the current one.
func (a *App) openPage(p page) {
	if p == a.page {
		return
	}
	a.parents = append(a.parents, a.page)
	a.showPage(p)
}

func (a *App) back() {
	a.removing = ""
	to := pageHome
	if n := len(a.parents); n > 0 {
		to, a.parents = a.parents[n-1], a.parents[:n-1]
	}
	a.showPage(to)
}

func roleLabel(role string) string {
	if role == audio.RoleCable {
		return "Others hear"
	}
	return "You hear"
}

// plural formats a count with a noun, e.g. "1 sound", "3 sounds".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// --- home ------------------------------------------------------------------

// issue is a problem worth showing at the top of the home page.
type issue struct {
	label, detail string
	tone          ui.Tone
	page          page
}

func (a *App) issues() []issue {
	var out []issue
	if detail, tone := a.outputsSummary(); tone == ui.ToneBad {
		out = append(out, issue{"Audio outputs", detail, tone, pageAudio})
	}
	if detail, tone := a.voiceSummary(); tone != ui.ToneGood {
		out = append(out, issue{"Voice chat", detail, tone, pageAudio})
	}
	return out
}

// homePage is everyday use: problems first, then the library, the output
// and volumes, and the phone. Everything else is under Settings.
func (a *App) homePage() []ui.Row {
	issues := a.issues()
	status := "Ready"
	switch n := len(issues); {
	case n == 1:
		status = "1 thing needs attention"
	case n > 1:
		status = fmt.Sprintf("%d things need attention", n)
	}
	// While sounds play: what plays, with a thin progress bar (like the
	// phone's top bar). Problems still have their own rows below.
	if text, ok := a.playingStatus(); ok {
		status = text
	}
	rows := []ui.Row{ui.Brand(a.logo, "RESOUND", "v"+Version, status, a.playbackProgress, a.brandUpdate())}
	for _, is := range issues {
		p := is.page
		rows = append(rows, ui.Nav(is.label, is.detail, is.tone, func() { a.openPage(p) }))
	}

	// Adding folders lives on the library page; home only refreshes.
	libDetail, libTone := a.librarySummary()
	var refresh func()
	if len(a.cfg.Folders) > 0 {
		refresh = a.refreshLibrary
	}
	rows = append(rows,
		ui.SectionLink("Sound library", "Import from Myinstants", func() { a.openPage(pageLibrary) }),
		ui.NavTool(ui.IconRefresh, "Refresh the library: picks up new, renamed and edited files", refresh,
			"Folders", libDetail, libTone, func() { a.openPage(pageLibrary) }),
	)
	if a.libNote != "" {
		rows = append(rows, ui.Paragraph(a.libNote, ui.ToneGood))
	}

	rows = append(rows, ui.Spacer(6))
	rows = append(rows, a.flowRows()...)

	remoteDetail, remoteTone := a.remoteSummary()
	rows = append(rows,
		ui.Section("Phone"),
		ui.NavTool(ui.IconExternalLink, "Open the app in a window on this PC", a.openRemoteHereAction(),
			"Phone remote", remoteDetail, remoteTone, func() { a.openPage(pageRemote) }),
		ui.Flex(),
		ui.Separator(),
		ui.Buttons(
			ui.Action{Label: "Settings", Icon: ui.IconSettings, OnClick: func() { a.showPage(pageSettings) }},
			ui.Action{Label: "Quit", Icon: ui.IconPower, Style: ui.ButtonDanger, Closes: true, OnClick: a.quit},
		),
		ui.Spacer(4),
	)
	return rows
}

// testActions play the test tone on both outputs and stop everything.
func (a *App) testActions() ui.Row {
	return ui.Buttons(
		ui.Action{Label: "Play test tone", Icon: ui.IconPlay, OnClick: func() { a.playTest(true) }},
		ui.Action{Label: "Stop all", Icon: ui.IconStop, OnClick: a.stopAll},
	)
}

// --- settings --------------------------------------------------------------

func (a *App) settingsPage() []ui.Row {
	audioDetail, audioTone := a.audioSummary()
	keys, keysTone := a.keysSummary()
	lib, libTone := a.librarySummary()
	remote, remoteTone := a.remoteSummary()
	appDetail, appTone := a.appSummary()
	backupDetail, backupTone := a.backupSummary()
	return []ui.Row{
		ui.Header("Settings", func() { a.showPage(pageHome) }),
		ui.Nav("Audio & voice chat", audioDetail, audioTone, func() { a.openPage(pageAudio) }),
		ui.Nav("Push-to-talk", keys, keysTone, func() { a.openPage(pageKeys) }),
		ui.Nav("Sound library", lib, libTone, func() { a.openPage(pageLibrary) }),
		ui.Nav("Phone remote", remote, remoteTone, func() { a.openPage(pageRemote) }),
		ui.Nav("App & startup", appDetail, appTone, func() { a.openPage(pageApp) }),
		ui.Nav("Backup & restore", backupDetail, backupTone, func() { a.openPage(pageBackup) }),
		ui.Nav("Diagnostics", "Latency · logs", ui.ToneMuted, func() { a.openPage(pageDiagnostics) }),
		ui.Flex(),
		ui.Separator(),
		ui.Button("Run the guided setup again", ui.ButtonNormal, false, a.startWizard),
		ui.Spacer(4),
	}
}

// --- outputs ---------------------------------------------------------------

func (a *App) outputsSummary() (string, ui.Tone) {
	running, failed := 0, 0
	for _, s := range a.engine.Status() {
		if s.Running {
			running++
		} else if s.Err != nil {
			failed++
		}
	}
	switch {
	case failed > 0:
		return "Error", ui.ToneBad
	case running == 2:
		return "Both running", ui.ToneGood
	case running == 1:
		return "1 running", ui.ToneWarning
	}
	return "None running", ui.ToneBad
}

// audioSummary is the Settings entry's status: a failed output first, then
// voice chat.
func (a *App) audioSummary() (string, ui.Tone) {
	if detail, tone := a.outputsSummary(); tone == ui.ToneBad {
		return "Output " + detail, tone
	}
	return a.voiceSummary()
}

// audioPage is the audio flow with everything that sets it up: installing
// VB-Cable, connecting it, and the options of what happens while sounds play.
func (a *App) audioPage() []ui.Row {
	rows := []ui.Row{
		ui.Header("Audio & voice chat", a.back),
		ui.Paragraph("Speak and play the test sound: the bars show what arrives where. "+
			"Others hear your microphone and sounds through VB-Cable; you hear the sounds yourself.", ui.ToneMuted),
	}
	rows = append(rows, a.flowRows()...)
	if !a.route.CableInstalled() {
		rows = append(rows, ui.Section("Install VB-Cable"))
		rows = append(rows, a.cableInstallRows()...)
	} else {
		rows = append(rows, a.cableNoteRows()...)
		rows = append(rows, a.connectRows()...)
		rows = append(rows, a.cableOutputHint()...)
	}

	rows = append(rows, ui.Section("While sounds play"))
	rows = append(rows, a.muteMicRows()...)
	keys, keysTone := a.keysSummary()
	rows = append(rows, ui.Nav("Push-to-talk key", keys, keysTone, func() { a.openPage(pageKeys) }))

	rows = append(rows, ui.Section("Devices"))
	if a.route.MicRouted && a.busy == "" {
		rows = append(rows, ui.Button("Stop routing microphone into the cable", ui.ButtonNormal, false, func() { a.setMicRouting(false) }))
	}
	rows = append(rows,
		ui.Buttons(
			ui.Action{Label: "Restart outputs", Icon: ui.IconRefresh, OnClick: a.restartOutputs},
			ui.Action{Label: "Windows sound", Icon: ui.IconExternalLink, Closes: true, OnClick: func() { _ = ui.ShellRun("control.exe", "mmsys.cpl,,1") }},
		),
		ui.Text("Restart outputs after plugging in a device.", "", ui.ToneMuted),
		ui.Section("Advanced _____"),
		a.cableOutputDropdown(),
		ui.Paragraph("Only for your own routing, e.g. a virtual mixer such as Voicemeeter. "+
			"Otherwise your sounds and your voice no longer meet in VB-Cable.", ui.ToneMuted),
	)
	rows = append(rows, a.cableCredit()...)
	return append(rows, ui.Spacer(4))
}

// --- voice chat (VB-Cable, microphone, push-to-talk) -------------------------

func (a *App) voiceSummary() (string, ui.Tone) {
	switch {
	case a.routeErr != nil && a.busy == "":
		return "Check failed", ui.ToneBad
	case a.cable.busy():
		return "Installing VB-Cable…", ui.ToneWarning
	case a.cable.state == cableRestart:
		return "Restart needed", ui.ToneWarning
	case !a.route.CableInstalled():
		return "VB-Cable missing", ui.ToneWarning
	}
	switch {
	case a.voiceReady():
		return "Ready", ui.ToneGood
	case !a.cableSelected():
		return "Sounds go elsewhere", ui.ToneWarning
	}
	return "Mic not routed", ui.ToneWarning
}

func (a *App) cableSelected() bool {
	return a.route.CableInput != nil && a.cableOutputID() == a.route.CableInput.ID
}

// voiceReady reports whether voice chat is connected: VB-Cable installed,
// sounds playing into it, and the microphone routed into it (or there is
// no microphone to route: the sounds still reach the cable).
func (a *App) voiceReady() bool {
	st := a.route
	return st.CableInstalled() && a.cableSelected() && (st.MicRouted || st.Mic == nil)
}

// --- push-to-talk -----------------------------------------------------------

func (a *App) keysSummary() (string, ui.Tone) {
	if len(a.cfg.PushToTalkKeys) == 0 {
		return "Not set", ui.ToneMuted
	}
	return a.pttText(), ui.ToneGood
}

func (a *App) keysPage() []ui.Row {
	rows := []ui.Row{ui.Header("Push-to-talk", a.back), a.pttIntro()}
	rows = append(rows, a.pttRows()...)
	rows = append(rows, a.hotkeyRows(capturePause)...)
	rows = append(rows, a.hotkeyRows(captureStop)...)
	return append(rows, ui.Spacer(4))
}

func (a *App) pttIntro() ui.Row {
	return ui.Paragraph("Games with push-to-talk only send your voice while its key is held. "+
		"Resound holds the key while sounds play, so others hear them.", ui.ToneMuted)
}

// muteMicRows are the option to mute the microphone while sounds play, with
// why one would.
func (a *App) muteMicRows() []ui.Row {
	return []ui.Row{
		ui.Toggle("Mute my microphone while sounds play", a.cfg.MuteMicWhilePlaying, a.toggleMuteWhilePlaying),
		ui.Paragraph("By default Resound mutes your mic during playback so others won't hear you talking in the background during music. You can disable this here.", ui.ToneMuted),
	}
}

// connectRows offer to connect VB-Cable in one go (sounds into CABLE Input,
// the microphone into the cable) while either is missing.
func (a *App) connectRows() []ui.Row {
	st := a.route
	var rows []ui.Row
	switch {
	case a.busy != "":
		rows = append(rows, ui.Text(a.busy, "", ui.ToneWarning))
	case !a.cableSelected() || (!st.MicRouted && st.Mic != nil):
		rows = append(rows,
			ui.Button("Connect to VB-Cable", ui.ButtonPrimary, false, a.connectCable),
			ui.Text("Windows asks for administrator approval once.", "", ui.ToneMuted))
	}
	if a.routeErr != nil {
		rows = append(rows, ui.Paragraph(a.routeErr.Error(), ui.ToneBad))
	}
	return rows
}

// cableOutputHint is the user's own step: their apps listen to the cable.
func (a *App) cableOutputHint() []ui.Row {
	return []ui.Row{
		ui.Section("In your apps"),
		ui.Paragraph("In Games, Discord or OBS choose “CABLE Output” as your microphone so others hear you and your sounds.", ui.ToneMuted),
	}
}

func (a *App) recheckCable() {
	a.refreshDevices(true)
	a.refreshRouting(true)
	a.render()
}

func (a *App) pttRows() []ui.Row {
	var rows []ui.Row
	if a.ptt.capturing && a.ptt.target == capturePTT {
		got := "…"
		if len(a.ptt.captured) > 0 {
			got = a.ptt.captured.String() + " …"
		}
		return append(rows,
			ui.Text("Press your push-to-talk key", got, ui.ToneWarning),
			ui.Text("Mouse buttons: press them over this panel. Esc cancels.", "", ui.ToneMuted))
	}
	presetOpts := make([]ui.Option, len(input.Presets))
	for i, p := range input.Presets {
		presetOpts[i] = ui.Option{Label: p.Keys.String(), Detail: p.Note}
	}
	rows = append(rows, ui.Dropdown("ptt-preset", "Pick a push to talk key", "Choose from these buttons or set your own…",
		presetOpts, a.presetIndex(), a.choosePreset))
	action := "Record"
	if len(a.cfg.PushToTalkKeys) > 0 {
		action = "Change"
	}
	rows = append(rows, ui.Item("Game push-to-talk key", a.pttText(), action, a.startPTTCapture))
	if len(a.cfg.PushToTalkKeys) == 0 {
		return rows
	}
	if a.ptt.sendIn > 0 {
		rows = append(rows, ui.Text(fmt.Sprintf("Sending %s in %d…", a.pttText(), a.ptt.sendIn), "", ui.ToneWarning))
	} else {
		rows = append(rows, ui.Button(fmt.Sprintf("Send it to the game in %d s", pttSendDelay), ui.ButtonNormal, false, a.sendPTTToGame))
	}
	return append(rows,
		ui.Text("Press, then click the game's keybind field.", "", ui.ToneMuted),
		ui.Toggle("Hold it while sounds play", a.cfg.PushToTalkWhilePlaying, a.togglePTTWhilePlaying),
		ui.Text("Talk-through on the phone always holds the PTT key.", "", ui.ToneMuted),
		ui.Button("Remove push-to-talk key", ui.ButtonNormal, false, func() { a.setPTTKeys(nil) }),
	)
}

// --- sound library ---------------------------------------------------------

func (a *App) librarySummary() (string, ui.Tone) {
	tabs, sounds := a.lib.Stats()
	switch {
	case len(a.cfg.Folders) == 0:
		return "No folders", ui.ToneWarning
	case sounds == 0:
		return "No sounds found", ui.ToneWarning
	}
	if st := a.indexStatus(); st.Busy {
		return fmt.Sprintf("Reading… %d / %d", st.Done, st.Total), ui.ToneWarning
	}
	return fmt.Sprintf("%s · %s", plural(sounds, "sound"), plural(tabs, "tab")), ui.ToneGood
}

func (a *App) indexStatus() library.IndexerStatus {
	if a.indexer == nil {
		return library.IndexerStatus{}
	}
	return a.indexer.Status()
}

func (a *App) libraryPage() []ui.Row {
	rows := []ui.Row{
		ui.Header("Sound library", a.back),
		ui.Paragraph("Each folder and each of its subfolders becomes a tab on the phone. Refresh after adding or renaming files.", ui.ToneMuted),
	}
	if len(a.cfg.Folders) > 0 {
		rows = append(rows,
			ui.Section("Import sounds"),
			ui.Paragraph("Find a sound on Myinstants, then drag its name or its link icon onto a folder below. Resound downloads it there. Links to MP3, WAV, FLAC and OGG files and sound files from Explorer work too.", ui.ToneMuted),
			ui.Buttons(ui.Action{Label: "Open Myinstants", Icon: ui.IconExternalLink, OnClick: a.openMyinstants}),
		)
	}
	if a.imports.warn != "" {
		rows = append(rows, ui.Paragraph(a.imports.warn, ui.ToneWarning))
	}
	if len(a.cfg.Folders) > 0 {
		rows = append(rows, ui.Section("Folders"))
	}
	rows = append(rows, a.folderRows()...)
	rows = append(rows, a.dropRows()...)
	rows = append(rows, a.libraryActions()...)
	switch st := a.indexStatus(); {
	case st.Busy:
		rows = append(rows, ui.Text(fmt.Sprintf("Reading new sounds… %d / %d", st.Done, st.Total), "", ui.ToneWarning))
	case st.Failed > 0:
		rows = append(rows, ui.Paragraph(fmt.Sprintf("%s without a readable length; it is measured when they play.", plural(st.Failed, "sound")), ui.ToneMuted))
	}
	return append(rows, ui.Spacer(4))
}

// folderRows lists the sound folders and their direct subfolders as drop
// fields: sounds dragged onto one are downloaded or copied into it (see
// import.go). Each library folder has a Remove action that asks for
// confirmation in place; clicking a field opens the folder in Explorer.
func (a *App) folderRows() []ui.Row {
	tabsByRoot := map[string][2]int{} // folder -> {tabs, sounds}
	soundsIn := map[string]int{}      // tab folder (lower case) -> sounds
	for _, t := range a.lib.Tabs() {
		c := tabsByRoot[t.Root]
		tabsByRoot[t.Root] = [2]int{c[0] + 1, c[1] + len(t.Sounds)}
		soundsIn[strings.ToLower(filepath.Clean(t.Path))] = len(t.Sounds)
	}
	open := func(dir string) func() {
		return func() {
			if err := ui.ShellOpen(dir); err != nil {
				logx.Printf("opening %s: %v", dir, err)
			}
		}
	}
	var rows []ui.Row
	for _, f := range a.cfg.Folders {
		folder := f
		c := tabsByRoot[filepath.Clean(f)]
		detail := fmt.Sprintf("%s · %s", plural(c[1], "sound"), plural(c[0], "tab"))
		if c[0] == 0 {
			detail = "No sounds yet"
		}
		rows = append(rows, ui.FolderDrop(ui.FolderSpec{
			Name: filepath.Base(f), Detail: detail, Tip: f,
			Action: "Remove", OnAction: func() { a.removing = folder; a.render() },
			OnClick: open(f), OnDrop: a.dropOnFolder(f),
		}))
		rows = append(rows, a.importRows(f)...)
		if a.removing == folder {
			rows = append(rows,
				ui.Paragraph("Remove this folder? Favorites, volumes and phone colors are kept for 30 days in case you add it back.", ui.ToneWarning),
				ui.Buttons(
					ui.Action{Label: "Cancel", OnClick: func() { a.removing = ""; a.render() }},
					ui.Action{Label: "Remove", Style: ui.ButtonDanger, OnClick: func() { a.removeFolder(folder) }},
				),
				ui.Separator())
		}
		for _, sub := range a.subfolders(f) {
			detail := "Empty"
			if n := soundsIn[strings.ToLower(sub)]; n > 0 {
				detail = plural(n, "sound")
			}
			rows = append(rows, ui.FolderDrop(ui.FolderSpec{
				Name: filepath.Base(sub), Detail: detail, Tip: sub, Sub: true,
				OnClick: open(sub), OnDrop: a.dropOnFolder(sub),
			}))
			rows = append(rows, a.importRows(sub)...)
		}
	}
	return rows
}

// dropRows is the drop field for sound folders, dragged in from Explorer
// (the panel stays open while another window has the focus).
func (a *App) dropRows() []ui.Row {
	return []ui.Row{
		ui.Spacer(4),
		ui.DropZone("Drop sound folders here", "or click to choose one", a.addFolder),
	}
}

// libraryActions adds a folder or refreshes the existing ones, and shows
// what the last of these found.
func (a *App) libraryActions() []ui.Row {
	addStyle := ui.ButtonNormal
	if len(a.cfg.Folders) == 0 {
		addStyle = ui.ButtonPrimary
	}
	rows := []ui.Row{ui.Buttons(
		ui.Action{Label: "Add folder…", Icon: ui.IconFolderPlus, Style: addStyle, OnClick: a.addFolder},
		ui.Action{Label: "Refresh", Icon: ui.IconRefresh, Disabled: len(a.cfg.Folders) == 0, OnClick: a.refreshLibrary},
	)}
	if a.libNote != "" {
		rows = append(rows, ui.Paragraph(a.libNote, ui.ToneGood))
	}
	return rows
}

// --- phone remote ----------------------------------------------------------

func (a *App) remoteSummary() (string, ui.Tone) {
	if a.srv == nil || a.serverErr != nil {
		return "Not running", ui.ToneBad
	}
	if n := a.srv.Auth().DeviceCount(); n > 0 {
		return plural(n, "device") + " paired", ui.ToneGood
	}
	return "Ready to pair", ui.ToneWarning
}

func (a *App) remotePage() []ui.Row {
	rows := []ui.Row{ui.Header("Phone remote", a.back)}
	pair, ok := a.pairRows()
	rows = append(rows, pair...)
	if ok {
		rows = append(rows,
			ui.Section("On this PC"),
			ui.Paragraph("The app also runs on this PC, for example as a button board on a second or touch screen.", ui.ToneMuted),
			ui.Buttons(ui.Action{Label: "Open the app on this PC", Icon: ui.IconExternalLink, OnClick: a.openRemoteHere}),
		)
	}
	if a.srv != nil {
		n := a.srv.Auth().DeviceCount()
		rows = append(rows,
			ui.Separator(),
			ui.Text("Paired devices", fmt.Sprint(n), ui.ToneNormal),
			ui.Button("Sign out all devices", ui.ButtonDanger, n == 0, a.resetRemote),
		)
	}
	rows = append(rows, a.firewallRows()...)
	return append(rows, ui.Spacer(4))
}

// pairRows show the pairing QR code. ok is false when phones can't connect;
// the rows then explain why.
func (a *App) pairRows() (rows []ui.Row, ok bool) {
	if a.srv == nil || a.serverErr != nil {
		msg := "Phones can't connect right now."
		if a.serverErr != nil {
			msg = a.serverErr.Error()
		}
		return []ui.Row{
			ui.Paragraph(msg, ui.ToneBad),
			ui.Text(fmt.Sprintf("UDP port %d may be used by another app.", a.cfg.ServerPort), "", ui.ToneMuted),
			ui.Button("Try again", ui.ButtonPrimary, false, func() { a.restartServer(); a.render() }),
		}, false
	}
	link, ok := a.srv.PairURL(a.appURL(), a.appAddresses())
	if !ok {
		return []ui.Row{ui.Text("No network connection found.", "", ui.ToneBad)}, false
	}
	rows = append(rows, ui.Section("Scan with your phone's camera"))
	if qr, err := qrcode.New(link, qrcode.Medium); err == nil {
		rows = append(rows, ui.QRCode(qr.Bitmap()))
	}
	return append(rows, ui.Paragraph("The Resound app opens; install it from there. "+
		"Your phone must be on the same Wi-Fi as this PC. Each code works once.", ui.ToneMuted)), true
}

// appAddresses are this PC's addresses in the QR code, most likely first.
// The app tries each, so a few are listed (each makes the code denser);
// virtual adapters' addresses only when there is nothing else.
func (a *App) appAddresses() []string {
	var main, other []string
	for _, ad := range a.addresses {
		if ad.Other {
			other = append(other, ad.IP)
		} else {
			main = append(main, ad.IP)
		}
	}
	if len(main) == 0 {
		main = other
	}
	return main[:min(len(main), 3)]
}

// openRemoteHereAction is openRemoteHere, or nil (disabled) while phones
// can't connect.
func (a *App) openRemoteHereAction() func() {
	if a.srv == nil || a.serverErr != nil {
		return nil
	}
	return a.openRemoteHere
}

// openRemoteHere opens the app on this PC with a pairing link. A browser
// that is paired already just connects; the code stays unused. Edge's app
// mode gives it a window of its own, sized like a phone; without Edge the
// default browser opens it.
func (a *App) openRemoteHere() {
	if a.srv == nil || a.serverErr != nil {
		return
	}
	url, ok := a.srv.PairURL(a.appURL(), a.appAddresses())
	if !ok {
		return
	}
	if err := ui.ShellRun("msedge.exe", "--app="+url+" --window-size=430,860"); err != nil {
		logx.Printf("app on this PC: Edge: %v; using the default browser", err)
		if err := ui.ShellOpen(url); err != nil {
			logx.Printf("app on this PC: %v", err)
		}
	}
}

func (a *App) firewallRows() []ui.Row {
	rows := []ui.Row{ui.Section("Phone can't connect?")}
	if a.fwBusy {
		rows = append(rows, ui.Text("Waiting for administrator approval…", "", ui.ToneWarning))
	} else {
		rows = append(rows, ui.Button("Allow phones through the firewall", ui.ButtonNormal, false, a.allowFirewall))
	}
	switch {
	case a.fwResult != "" && a.fwFailed:
		rows = append(rows, ui.Text(a.fwResult, "", ui.ToneBad))
	case a.fwResult != "":
		rows = append(rows, ui.Text(a.fwResult, "", ui.ToneGood))
	default:
		rows = append(rows, ui.Text("Needs administrator approval once.", "", ui.ToneMuted))
	}
	return rows
}
