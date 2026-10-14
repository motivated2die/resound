// Package app wires configuration, the audio engine, the Windows routing
// helpers and the tray UI together.
package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/motivated2die/resound/assets"
	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/install"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
	"github.com/motivated2die/resound/internal/server"
	"github.com/motivated2die/resound/internal/ui"
)

// Version is set at build time with -ldflags "-X .../internal/app.Version=...".
var Version = "0.13.0-dev"

// AppURL is the app's address (webapp/, built by cmd/webapp).
// Set at build time with -ldflags "-X .../internal/app.AppURL=..."; the
// RESOUND_APP_URL environment variable and config.json's appURL replace it.
var AppURL = "https://getresound.app"

const appURLEnv = "RESOUND_APP_URL"

// appURL returns the app's address in use.
func (a *App) appURL() string {
	if a.cfg.AppURL != "" {
		return a.cfg.AppURL
	}
	if u := os.Getenv(appURLEnv); u != "" {
		return u
	}
	return AppURL
}

type page int

const (
	pageHome   page = iota
	pageWizard      // guided first-run setup, step in App.wizardStep
	pageSettings
	pageAudio // the audio flow, VB-Cable and the microphone
	pageKeys  // push-to-talk
	pageLibrary
	pageRemote
	pageApp
	pageBackup
	pageDiagnostics
)

// App holds the running host state. Everything except the event pump and
// the elevated-helper goroutine runs on the UI thread.
type App struct {
	cfg    config.Config
	engine *audio.Engine
	tray   *ui.Tray
	logo   *ui.Image // header logo, nil if it can't be decoded
	page   page

	parents    []page // where detail pages' back buttons return to, innermost last
	wizardStep int    // current step of the guided setup (pageWizard)

	devices     []audio.Device
	devErr      error
	outputsOpen bool   // openOutputs ran: the automatic cable output follows VB-Cable
	cableOpenID string // the device the cable output was last opened on

	route    routing.Status
	routeErr error
	busy     string // non-empty while waiting for the elevated helper
	cable    cableInstall

	playback   playbackState
	ptt        pttState
	levels     levelState // the audio flow's meters (flow.go)
	flowOnPage bool       // the current page shows the audio flow

	lib      *library.Library
	indexer  *library.Indexer
	removing string       // folder awaiting the remove confirmation
	libNote  string       // result of the last refresh or add, shown for a moment (setLibNote)
	libSeq   int          // identifies the current libNote, so an old timer leaves a newer one
	keepNote bool         // reopening after a dialog: keep libNote and backupNote
	imports  importsState // sounds dropped onto folders (import.go)

	backupNote       string // result of the last backup action
	backupFailed     bool
	restoreCandidate string    // backup awaiting the restore confirmation
	lastIndex        time.Time // last panel refresh for indexer progress
	watching         bool      // animate is running
	wasPlaying       bool      // playback state at the last tick
	wasHeld          string    // heldKey at the last tick
	srv              *server.Server
	serverErr        error
	addresses        []server.Address // in the pairing QR code

	portable bool   // development build: never installs itself
	fwBusy   bool   // waiting for the elevated firewall helper
	fwResult string // last firewall helper result
	fwFailed bool

	update updater // check for and install new releases (update.go)

	hotkeys   *input.Watcher // pause and stop keys (hotkeys.go); nil while none is set
	hotkeyErr string
}

// Run starts the host (or the elevated routing helper) and blocks until it
// ends. It returns the process exit code.
func Run() int {
	closeLog, err := logx.Init(config.Dir())
	defer closeLog()
	if err != nil {
		logx.Printf("file logging unavailable: %v", err)
	}

	ui.InitDPIAwareness()

	// Helper commands: the same executable with a special first argument.
	// They run once and exit, and are not subject to single-instance.
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case routing.ApplyCommand: // elevated
			return routing.RunApplyCommand(args[1:])
		case install.FirewallCommand: // elevated
			return install.RunFirewallCommand()
		case install.UninstallCommand:
			return install.RunUninstall(config.Dir(), closeLog)
		}
	}
	welcome := ""
	for _, arg := range args {
		if v, ok := strings.CutPrefix(arg, install.WelcomeFlag); ok {
			welcome = v
		}
	}

	logx.Printf("Resound %s starting %q", Version, args) // the arguments tell who started it

	// Drop-in install: a downloaded exe installs itself, starts the
	// installed copy and exits. Development builds stay portable.
	portable := install.Portable()
	if portable {
		logx.Printf("portable mode: not installing (development build)")
	} else if exit, err := install.Ensure(Version, config.Dir()); err != nil {
		logx.Printf("install failed, running from here: %v", err)
		ui.ShowError(0, "Resound", "Resound could not be installed and runs from its current location:\n\n"+err.Error())
	} else if exit {
		logx.Printf("install: done, the installed copy takes over")
		return 0
	}

	// A restore requested by the previous instance (see backup.go).
	restoredTitle, restoredNote := applyPendingRestore(args)

	// Single instance: a second start opens the running instance's panel.
	mutex, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr("Local\\Resound.SingleInstance"))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		logx.Printf("already running; signalling the other instance")
		ui.SignalRunningInstance()
		return 0
	}
	if mutex != 0 {
		defer windows.CloseHandle(mutex)
	}

	a := &App{cfg: config.Load(), engine: audio.NewEngine(), portable: portable}
	a.migrateSetupStep()
	// Configured before the guided setup existed. A setup in progress has
	// a step and may have folders already (e.g. after VB-Cable's restart).
	if !a.cfg.SetupComplete && a.cfg.SetupStep == 0 && (len(a.cfg.Folders) > 0 || a.cfg.CableDeviceID != "") {
		a.cfg.SetupComplete = true
		a.save()
	}
	if !a.cfg.SetupComplete {
		a.page, a.wizardStep = pageWizard, max(0, min(a.cfg.SetupStep, wizardSteps))
	}
	a.lib = library.Open(config.Dir(), audio.SupportedExtensions)
	a.lib.Logf = logx.Printf
	defer a.lib.Close()
	a.migrateVolumeScale()
	a.playback.init()
	a.ptt.init()
	a.imports.init()
	defer a.engine.Close()

	a.tray, err = ui.NewTray("Resound")
	if err != nil {
		logx.Printf("fatal: %v", err)
		ui.ShowError(0, "Resound", "Could not create the tray icon:\n"+err.Error())
		return 1
	}
	if a.logo, err = ui.NewImage(assets.LogoPNG); err != nil {
		logx.Printf("logo: %v", err)
	}
	a.tray.Popup().OnShow = a.onPanelOpen
	a.tray.Popup().OnDrop = a.onDropAnywhere // drops on a folder go to its own field
	a.tray.Popup().OnKey = a.onPanelKey
	a.tray.Popup().OnEscape = a.onEscape
	a.tray.OnQuitRequest = func() {
		logx.Printf("quit requested by another process (update or uninstall)")
		a.quit()
	}
	a.tray.OnEndSession = func() {
		logx.Printf("Windows is ending the session")
		a.prepareForShutdown()
	}

	// Step markers make the last successful phase visible in the log.
	logx.Printf("startup: listing outputs")
	a.refreshDevices(true)
	logx.Printf("startup: checking routing")
	a.refreshRouting(true)
	cableTitle, cableNote, cableConfirm := a.resumeCableInstall()
	logx.Printf("startup: opening outputs")
	a.applyVolumes()
	a.openOutputs()
	go a.pumpEvents()
	logx.Printf("startup: scanning library")
	a.rescan()
	a.startIndexer()
	logx.Printf("startup: starting remote server")
	a.startServer()

	a.startUpdateChecks()
	a.applyHotkeys()

	a.updateTooltip()
	logx.Printf("ready")
	switch {
	case restoredNote != "":
		go a.showNotice(restoredTitle, restoredNote, "Open panel")
	case welcome != "":
		go a.showWelcome(welcome)
	case cableNote != "":
		go a.showNotice(cableTitle, cableNote, cableConfirm)
	}
	a.tray.Run()

	a.closeLevels()
	a.closeHotkeys()
	a.releasePlayback() // never leave the mic muted or a key held behind
	a.releaseAllPTT()
	a.save()
	logx.Printf("exited")
	return 0
}

// onPanelOpen refreshes device state each time the panel opens. The page
// is kept, so a setup in progress continues where the user left it.
func (a *App) onPanelOpen() {
	a.ptt.capturing = false // a capture ends when the panel closes
	if !a.keepNote {
		a.libNote, a.backupNote = "", ""
		a.imports.clearFinished()
	}
	a.refreshDevices(false)
	a.refreshRouting(false)
	a.addresses = server.LocalAddresses()
	a.wasPlaying = len(a.engine.Voices()) > 0
	a.render()
}

// onEscape handles Esc while the panel is open, focused or not: it cancels
// recording a key, or goes back a page as the back button would, up to the
// home page. It returns false where the panel should close: on the home
// page and in the guided setup (which keeps its step). Closing keeps the
// page, so the tray icon reopens it where the user was.
func (a *App) onEscape() bool {
	switch {
	case a.ptt.capturing:
		a.ptt.capturing = false
		a.render()
	case a.page == pageHome || a.page == pageWizard:
		return false
	case a.page == pageSettings:
		a.showPage(pageHome)
	default:
		a.back()
	}
	return true
}

// reopenPanel shows the panel again after a dialog (folder or file picker)
// closed it, so the user continues where they were and sees the result.
func (a *App) reopenPanel() {
	a.keepNote = true
	a.tray.ShowPanel() // runs onPanelOpen synchronously
	a.keepNote = false
}

func (a *App) save() {
	if err := config.Save(a.cfg); err != nil {
		logx.Printf("saving config: %v", err)
	}
}

func (a *App) quit() {
	a.cancelCableDownload()
	a.cancelUpdate()
	a.imports.cancel()
	if a.srv != nil {
		a.srv.Close() // before the UI thread stops answering server calls
	}
	a.engine.Close()
	a.tray.Quit()
}

func (a *App) showPage(p page) {
	if p == pageHome {
		a.parents = nil
	}
	a.page = p
	a.tray.Popup().ScrollToTop()
	a.render()
}

// render rebuilds the panel rows from the current state.
func (a *App) render() {
	a.updateTooltip()
	a.flowOnPage = false // set again by flowRows
	a.tray.Popup().SetRows(a.rows())
	a.tray.Popup().SetAcceptDrops(a.acceptsDrops()) // folders can be dragged in
	if a.flowOnPage || (a.page == pageHome && len(a.engine.Voices()) > 0) {
		a.animate() // the flow's level bars, the progress bar under the title
	}
}

func (a *App) updateTooltip() {
	running := 0
	for _, s := range a.engine.Status() {
		if s.Running {
			running++
		}
	}
	tip := fmt.Sprintf("Resound · %d output(s) running", running)
	if a.route.CableInstalled() && !a.route.MicRouted {
		tip += " · mic not routed"
	}
	a.tray.SetTooltip(tip)
}

// pumpEvents forwards engine events to the UI thread. It runs on its own
// goroutine for the lifetime of the process.
func (a *App) pumpEvents() {
	for ev := range a.engine.Events() {
		a.tray.Post(func() { a.onEngineEvent(ev) })
	}
}

// showWelcome is the notice after an install or update. The tray icon is
// usually hidden behind the taskbar's arrow, so the notice sits in that
// corner and says where to look. Runs on its own goroutine.
func (a *App) showWelcome(kind string) {
	title, msg, confirm := "Resound is installed",
		"It runs in the background from the tray. A short guided setup adds your sounds, checks your audio devices and connects your phone.",
		"Start setup"
	if kind == install.WelcomeUpdate {
		title, msg, confirm = "Resound is updated", "Updated to v"+Version+" and running.", "Open panel"
	}
	a.showNotice(title, msg, confirm)
}

// showNotice shows a passive notice in the tray corner, with a button that
// opens the panel. Runs on its own goroutine.
func (a *App) showNotice(title, msg, confirm string) {
	open, shown := ui.TryConfirm(ui.DialogSpec{
		Title:    title,
		Message:  msg,
		TrayHint: "Its icon may be hidden behind this arrow next to the clock. Drag it onto the taskbar to keep it in sight.",
		Confirm:  confirm,
		Cancel:   "Got it",
		Corner:   true,
		Passive:  true,
	})
	if !shown {
		logx.Printf("notice %q could not be shown", title)
		return
	}
	if open {
		a.tray.Post(a.tray.ShowPanel)
	}
}
