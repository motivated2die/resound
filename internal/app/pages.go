package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/motivated2die/resound/internal/audio"
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
	case pageOverlay:
		return a.overlayPage()
	case pageAppKeys:
		return a.appKeysPage()
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

// homePage is everyday use: problems first, then the library and the web
// app side by side, then the output and volumes. Everything else is under
// Settings.
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
	if keys := a.runningKeysStatus(); keys != "" && len(issues) == 0 {
		status = "Ready · " + keys // an app with keybinds of its own runs: they apply while it is in front
	}
	if text, ok := a.playingStatus(); ok {
		status = text
	}
	rows := []ui.Row{ui.Brand(a.logo, "RESOUND", "v"+Version, status, a.playbackProgress, a.brandUpdate())}
	for _, is := range issues {
		p := is.page
		rows = append(rows, ui.Nav(is.label, is.detail, is.tone, func() { a.openPage(p) }))
	}

	rows = append(rows, a.homeCards())
	if a.libNote != "" {
		rows = append(rows, ui.Paragraph(a.libNote, ui.ToneGood))
	}

	rows = append(rows, ui.Spacer(12)) // sets the cards apart from the flow
	rows = append(rows, a.flowRows()...)

	rows = append(rows,
		ui.Flex(),
		ui.Separator(),
		ui.Buttons(
			ui.Action{Label: "Help", Icon: ui.IconHelp, Closes: true, OnClick: a.openHelp},
			ui.Action{Label: "Settings", Icon: ui.IconSettings, OnClick: func() { a.showPage(pageSettings) }},
			ui.Action{Label: "Quit", Icon: ui.IconPower, Style: ui.ButtonDanger, Closes: true, OnClick: a.quit},
		),
		ui.Spacer(4),
	)
	return rows
}

// homeCards are the sound library and the web app side by side. Adding
// folders and importing sounds live on the library page; home only
// refreshes.
func (a *App) homeCards() ui.Row {
	libDetail, libTone := a.librarySummary()
	var refresh func()
	if len(a.cfg.Folders) > 0 {
		refresh = a.refreshLibrary
	}
	remoteDetail, remoteTone := a.remoteSummary()
	return ui.PageCards(
		ui.PageCard{
			Section: "Sound library", Title: "Folders",
			Detail: libDetail, Tone: libTone, Art: ui.IconFileMusic,
			OnClick: func() { a.openPage(pageLibrary) }, Action: "Import sounds",
			Tool: ui.IconRefresh, ToolTip: "Refresh the library: picks up new, renamed and edited files",
			ToolAction: "Refresh library", OnTool: refresh,
		},
		ui.PageCard{
			Section: "Remote", Title: "Web app",
			Detail: remoteDetail, Tone: remoteTone, Art: ui.IconLayoutDash,
			OnClick: func() { a.openPage(pageRemote) }, Action: "Pair new device",
			Tool: ui.IconExternalLink, ToolTip: "Open the app in a window on this PC",
			ToolAction: "Open remote", OnTool: a.openRemoteHereAction(),
		},
	)
}

// openHelp opens the help center (setup guides, features, troubleshooting)
// in the browser: /help/ on the app's site (webapp/help, built by
// cmd/webapp), which the installed app doesn't cache.
func (a *App) openHelp() { a.openHelpAt("") }

// Help articles the panel links to (webapp/help/<article>.md, an optional
// #heading). TestAppHelpLinks in cmd/webapp checks they exist.
const (
	helpOwnMixer      = "voicemeeter"
	helpBeforeVBCable = "voicemeeter#before-you-install-vb-cable"
	helpKeybinds      = "push-to-talk"
)

// openHelpAt opens one help article ("" = the help center's start page).
func (a *App) openHelpAt(article string) {
	url := strings.TrimSuffix(a.appURL(), "/") + "/help/" + article
	if err := ui.ShellOpen(url); err != nil {
		logx.Printf("help: %v", err)
	}
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
	overlayDetail, overlayTone := a.overlaySummary()
	return []ui.Row{
		ui.Header("Settings", func() { a.showPage(pageHome) }),
		ui.Nav("Audio & voice chat", audioDetail, audioTone, func() { a.openPage(pageAudio) }),
		ui.Nav("Keybinds", keys, keysTone, func() { a.openPage(pageKeys) }),
		ui.Nav("Sound library", lib, libTone, func() { a.openPage(pageLibrary) }),
		ui.Nav("In-game overlay", overlayDetail, overlayTone, func() { a.openPage(pageOverlay) }),
		ui.Nav("Remote App", remote, remoteTone, func() { a.openPage(pageRemote) }),
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
	through := "VB-Cable"
	if a.ownRouting() {
		through = "your own routing"
	}
	rows := []ui.Row{
		ui.Header("Audio & voice chat", a.back),
		ui.Paragraph("Speak and play the test sound: the bars show what arrives where. "+
			"Others hear your microphone and sounds through "+through+"; you hear the sounds yourself.", ui.ToneMuted),
	}
	rows = append(rows, a.flowRows()...)
	if a.ownRouting() {
		rows = append(rows, a.cableOutputHint()...)
	} else if !a.route.CableInstalled() {
		rows = append(rows, ui.Section("Install VB-Cable"))
		rows = append(rows, a.cableInstallRows()...)
	} else {
		rows = append(rows, a.cableNoteRows()...)
		rows = append(rows, a.connectRows()...)
		rows = append(rows, a.cableOutputHint()...)
	}

	rows = append(rows, ui.Section("While sounds play"))
	rows = append(rows, a.overlapRows()...)
	rows = append(rows, a.muteMicRows()...)
	keys, keysTone := a.keysSummary()
	rows = append(rows, ui.Nav("Keybinds", keys, keysTone, func() { a.openPage(pageKeys) }))

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
	)
	rows = append(rows, a.ownRoutingRows()...)
	rows = append(rows, a.cableCredit()...)
	return append(rows, ui.Spacer(4))
}

// ownRoutingRows are the Advanced section: sounds for others into a device
// of the user's own routing (e.g. a Voicemeeter input) instead of VB-Cable,
// and the device others' apps then listen to. Resound never reads or
// changes the mixer; the short how-to is all it gives.
func (a *App) ownRoutingRows() []ui.Row {
	rows := []ui.Row{ui.Separator(), ui.Section("Advanced"), a.cableOutputDropdown()}
	if !a.ownRouting() {
		rows = append(rows, ui.Paragraph("Only for your own routing, e.g. a virtual mixer such as Voicemeeter. "+
			"Otherwise your sounds and your voice no longer meet in VB-Cable.", ui.ToneMuted))
		if a.route.Voicemeeter {
			rows = append(rows, ui.Paragraph("Voicemeeter is installed. To use it instead of VB-Cable, choose one of its inputs above, "+
				"e.g. Voicemeeter AUX Input.", ui.ToneMuted))
		}
		return rows
	}
	rows = append(rows,
		ui.Warning("Non-standard setup: click for help",
			"Your sounds for others don't go to VB-Cable: your own mixer has to combine them with your microphone. Click for the setup guide.",
			func() { a.openHelpAt(helpOwnMixer) }),
		ui.Paragraph("Your mixer has to combine your microphone and these sounds into one output. Choose that output here and as the microphone in your apps.", ui.ToneMuted),
		a.appsMicDropdown(),
	)
	if a.route.Voicemeeter {
		rows = append(rows, ui.Paragraph("Voicemeeter Banana: choose your mic as Hardware Input 1. Turn on B1 for it and for the strip Resound plays into, "+
			"and turn A1 off on that strip, or you hear the sounds twice. Then choose “Voicemeeter Out B1” here. Keep Voicemeeter running.", ui.ToneMuted))
	}
	return append(rows,
		ui.Paragraph("Under “Your microphone”, choose the mic your mixer records, so Resound mutes it while sounds play.", ui.ToneMuted),
		ui.Button("Back to VB-Cable", ui.ButtonNormal, false, a.useCableAsOutput),
	)
}

// appsMicDropdown chooses the recording device others' apps use as their
// microphone with an own routing: where the mixer sends voice and sounds.
func (a *App) appsMicDropdown() ui.Row {
	var opts []ui.Option
	var ids []string
	sel := -1
	for _, in := range a.route.Inputs {
		if a.route.Mic != nil && in.ID == a.route.Mic.ID && a.cfg.MicDeviceID != "" {
			continue // the chosen real mic
		}
		o := ui.Option{Label: in.Name}
		if in.IsVoicemeeter() {
			o.Detail = "Voicemeeter"
		}
		if in.ID == a.cfg.AppsMicDeviceID {
			sel = len(opts)
		}
		opts = append(opts, o)
		ids = append(ids, in.ID)
	}
	placeholder := "Choose a recording device…"
	if a.cfg.AppsMicDeviceID != "" && sel < 0 {
		placeholder = "Not connected: choose another one…"
	}
	return ui.Dropdown("apps-mic", "Others' apps listen to", placeholder, opts, sel, func(i int) { a.selectAppsMic(ids[i]) })
}

// --- voice chat (VB-Cable, microphone, push-to-talk) -------------------------

func (a *App) voiceSummary() (string, ui.Tone) {
	switch {
	case a.routeErr != nil && a.busy == "":
		return "Check failed", ui.ToneBad
	case a.ownRouting() && a.appsMic() == nil:
		return "Choose your mixer's output", ui.ToneWarning
	case a.ownRouting():
		return "Own routing", ui.ToneGood
	case a.cable.busy():
		return "Installing VB-Cable…", ui.ToneWarning
	case a.cable.state == cableRestart:
		return "Restart needed", ui.ToneWarning
	case !a.route.CableInstalled():
		return "VB-Cable missing", ui.ToneWarning
	}
	if a.voiceReady() {
		return "Ready", ui.ToneGood
	}
	return "Mic not routed", ui.ToneWarning
}

func (a *App) cableSelected() bool {
	return a.route.CableInput != nil && a.cableOutputID() == a.route.CableInput.ID
}

// voiceReady reports whether voice chat is connected: VB-Cable installed,
// sounds playing into it, and the microphone routed into it (or there is
// no microphone to route: the sounds still reach the cable). With an own
// routing, the mixer does the routing: ready once the device others' apps
// listen to is chosen and present.
func (a *App) voiceReady() bool {
	if a.ownRouting() {
		return a.appsMic() != nil
	}
	st := a.route
	return st.CableInstalled() && a.cableSelected() && (st.MicRouted || st.Mic == nil)
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
	name := "CABLE Output"
	if a.ownRouting() {
		m := a.appsMic()
		if m == nil {
			return nil // the flow and the Advanced section ask for it
		}
		name = shortDeviceName(m.Name)
	}
	return []ui.Row{
		ui.Section("In your apps"),
		ui.Paragraph("In Games, Discord or OBS choose “"+name+"” as your microphone so others hear you and your sounds.", ui.ToneMuted),
	}
}

func (a *App) recheckCable() {
	a.refreshDevices(true)
	a.refreshRouting(true)
	a.render()
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
	var refresh func()
	if len(a.cfg.Folders) > 0 {
		refresh = a.refreshLibrary
	}
	rows := []ui.Row{
		ui.HeaderTool("Sound library", a.back, ui.IconRefresh,
			"Refresh the library: picks up new folders, new, renamed and edited files", refresh),
		ui.Paragraph("Each folder and each subfolder with sounds becomes a tab on the phone. Refresh after changing files or folders in Explorer.", ui.ToneMuted),
	}
	if len(a.cfg.Folders) > 0 {
		rows = append(rows,
			ui.Section("Import sounds"),
			ui.ImportGuide("Find a sound on a button site and drag its name onto a folder below. Resound downloads it there. Sound files from Explorer work too; videos open in an editor to cut a clip.", "drag its name"),
			ui.Buttons(
				ui.Action{Label: "Myinstants", OnClick: a.openSite(myinstantsURL)},
				ui.Action{Label: "Sound Buttons World", OnClick: a.openSite(soundButtonsWorldURL)},
			),
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
	rows = append(rows,
		ui.Section("Loudness"),
		ui.Toggle("Even out the loudness of sounds", !a.cfg.NoNormalize, a.toggleNormalize),
		ui.Paragraph("Loud sounds are turned down and quiet ones up, so every sound plays about equally loud. Volumes you set for a sound apply on top.", ui.ToneMuted),
	)
	return append(rows, ui.Spacer(4))
}

// folderRows lists the sound folders and all their subfolders (empty ones
// too, as the last scan found them) as drop fields: sounds dragged onto one
// are downloaded or copied into it (see import.go), and the field shows how
// that goes. Each library folder has a trash button that asks for
// confirmation in place. Clicking a field opens its list of sounds below it
// (sounds.go); under the pointer, a button on the field opens the folder in
// Explorer.
func (a *App) folderRows() []ui.Row {
	tabsByRoot := map[string][2]int{} // folder -> {tabs, sounds}
	for _, t := range a.lib.Tabs() {
		c := tabsByRoot[t.Root]
		tabsByRoot[t.Root] = [2]int{c[0] + 1, c[1] + len(t.Sounds)}
	}
	soundsIn := a.soundsByFolder()
	toggle := func(dir string) func() { return func() { a.toggleFolder(dir) } }
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
		note, active := a.folderImport(f)
		rows = append(rows, a.importGroup(active, ui.FolderDrop(ui.FolderSpec{
			Name: filepath.Base(f), Detail: detail, Tip: f, Note: note, Active: active,
			ActionIcon: ui.IconTrash, ActionTip: "Remove from the library (the folder and its files stay on disk)",
			OnAction: func() { a.removing = folder; a.render() },
			OnClick:  toggle(f), Expanded: a.lists.isOpen(f), OnOpen: open(f),
			OnDrop: a.dropOnFolder(f), DropHint: dropHint,
		}), a.importRows(f))...)
		if a.removing == folder {
			rows = append(rows,
				ui.Paragraph("Remove this folder from the library? Its files stay on disk. Favorites, volumes and phone colors are kept for 30 days in case you add it back.", ui.ToneWarning),
				ui.Buttons(
					ui.Action{Label: "Cancel", OnClick: func() { a.removing = ""; a.render() }},
					ui.Action{Label: "Remove", Style: ui.ButtonDanger, OnClick: func() { a.removeFolder(folder) }},
				),
				ui.Separator())
		}
		if a.lists.isOpen(f) {
			rows = append(rows, a.soundRows(soundsIn[folderKey(f)], 1)...)
		}
		for _, sub := range a.lib.Folders(f) {
			detail := "Empty"
			if n := len(soundsIn[folderKey(sub.Path)]); n > 0 {
				detail = plural(n, "sound")
			}
			note, active := a.folderImport(sub.Path)
			rows = append(rows, a.importGroup(active, ui.FolderDrop(ui.FolderSpec{
				Name: filepath.Base(sub.Path), Detail: detail, Tip: sub.Path, Depth: sub.Depth,
				Note: note, Active: active, OnClick: toggle(sub.Path), Expanded: a.lists.isOpen(sub.Path), OnOpen: open(sub.Path),
				OnDrop: a.dropOnFolder(sub.Path), DropHint: dropHint,
			}), a.importRows(sub.Path))...)
			if a.lists.isOpen(sub.Path) {
				rows = append(rows, a.soundRows(soundsIn[folderKey(sub.Path)], sub.Depth+1)...)
			}
		}
	}
	return rows
}

// dropRows is the drop field for sound folders, dragged in from Explorer
// (the panel stays open while another window has the focus), under its own
// heading so it stands apart from the folders above.
func (a *App) dropRows() []ui.Row {
	return []ui.Row{
		ui.Section("Add folders"),
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

// --- Remote App (the phone remote) -------------------------------------------

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
	rows := []ui.Row{ui.Header("Remote App", a.back)}
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
		devices := a.pairedDeviceRows()
		rows = append(rows, ui.Separator(), ui.Section("Paired devices"))
		rows = append(rows, devices...)
		rows = append(rows, ui.Button("Sign out all devices", ui.ButtonDanger, len(devices) == 0, a.resetRemote))
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
	copyAction := ui.Action{Label: "Copy link", Icon: ui.IconCopy, OnClick: a.copyPairLink}
	if a.linkCopied > 0 {
		copyAction.Label, copyAction.Icon = "Link copied", ui.IconCheck
	}
	rows = append(rows, ui.Buttons(copyAction))
	if a.linkCopyErr != "" {
		rows = append(rows, ui.Paragraph(a.linkCopyErr, ui.ToneBad))
	}
	return append(rows, ui.Paragraph("The Resound app opens; install it from there. "+
		"Your phone must be on the same Wi-Fi as this PC. Each code or copied link works once.", ui.ToneMuted)), true
}

// linkCopiedTime is how long the copy button says "Link copied".
const linkCopiedTime = 2 * time.Second

// copyPairLink puts the pairing link, the one in the QR code, on the
// clipboard, to open on another device or in a development build of the
// app. It holds a pairing code, so it is never logged.
func (a *App) copyPairLink() {
	if a.srv == nil || a.serverErr != nil {
		return
	}
	link, ok := a.srv.PairURL(a.appURL(), a.appAddresses())
	if !ok {
		return
	}
	if err := ui.SetClipboardText(a.tray.Hwnd(), link); err != nil {
		logx.Printf("copying the pairing link: %v", err)
		a.linkCopyErr = "Couldn't copy the link: " + err.Error()
		a.render()
		return
	}
	logx.Printf("pairing link copied")
	a.linkCopyErr = ""
	a.linkCopied++
	n := a.linkCopied
	time.AfterFunc(linkCopiedTime, func() {
		a.tray.Post(func() {
			if a.linkCopied == n { // not copied again meanwhile
				a.linkCopied = 0
				a.render()
			}
		})
	})
	a.render()
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
