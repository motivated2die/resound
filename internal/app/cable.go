package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/install"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
	"github.com/motivated2die/resound/internal/ui"
	"github.com/motivated2die/resound/internal/vbcable"
)

// Installing VB-Cable for the user (see package vbcable): download the
// official pack, run its setup silently with administrator approval, put
// back default devices Windows moved to the cable, then test the cable with a
// quiet tone. If the tone comes through, it's done. If not, the panel offers
// the restart the driver may need, and, while the device itself waits, a try
// without one. A RunOnce entry and the pending-install file bring the app back
// after the restart, on the page the install started from.

const vbDonateURL = "https://vb-audio.com/Services/licensing.htm"

type cableState int

const (
	cableIdle        cableState = iota
	cableDownloading            // fetching and checking the driver pack
	cableInstalling             // the setup runs (UAC and driver prompts)
	cableChecking               // waiting for the devices to appear, testing the cable
	cableWaking                 // restarting the cable's device (UAC prompt)
	cableRestart                // a restart is needed or recommended
)

type cableInstall struct {
	state   cableState
	percent int    // download progress, -1 while unknown
	err     string // why the last attempt failed, shown with the manual way
	note    string // what the install did, e.g. restored default devices
	confirm bool   // the restart confirmation is shown
	// detected: nothing was installed from here, the cable was found waiting
	// for a restart. wakeFailed: restarting the device didn't help.
	detected, wakeFailed bool
	cancel               context.CancelFunc
}

func (c cableInstall) busy() bool {
	return c.state == cableDownloading || c.state == cableInstalling || c.state == cableChecking || c.state == cableWaking
}

// installCable starts the automatic install.
func (a *App) installCable() {
	if a.cable.state != cableIdle {
		return
	}
	a.syncCableHealth() // never install over a cable that waits for a restart
	if a.cable.state != cableIdle {
		a.render()
		return
	}
	returnTo, step := vbcable.ReturnVoice, 0
	if a.page == pageWizard {
		returnTo, step = vbcable.ReturnWizard, a.wizardStep
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cable = cableInstall{state: cableDownloading, percent: -1, cancel: cancel}
	a.render()
	logx.Printf("vbcable: automatic install started")

	dataDir := config.Dir()
	go func() {
		defer cancel()
		defaults, err := vbcable.SnapshotDefaults()
		if err != nil {
			logx.Printf("vbcable: reading default devices: %v", err)
		}
		lastPct, lastPost := -1, time.Time{}
		setup, err := vbcable.Download(ctx, vbcable.Dir(dataDir), func(done, total int64) {
			if total <= 0 {
				return
			}
			pct := int(done * 100 / total)
			if pct == lastPct || (pct < 100 && time.Since(lastPost) < 150*time.Millisecond) {
				return
			}
			lastPct, lastPost = pct, time.Now()
			a.tray.Post(func() {
				if a.cable.state == cableDownloading {
					a.cable.percent = pct
					a.render()
				}
			})
		})
		if err != nil {
			_ = os.RemoveAll(vbcable.Dir(dataDir))
			if ctx.Err() != nil {
				logx.Printf("vbcable: download cancelled")
				a.tray.Post(func() { a.cable = cableInstall{}; a.render() })
				return
			}
			a.cableFailed(err)
			return
		}
		a.tray.Post(func() { a.cable.state = cableInstalling; a.render() })

		// Recorded before the setup runs, so an app or Windows restart in
		// the middle still finds its way back.
		p := vbcable.Pending{Started: time.Now(), BootTime: vbcable.BootTime(), ReturnTo: returnTo, WizardStep: step, Defaults: defaults}
		if err := vbcable.SavePending(dataDir, p); err != nil {
			logx.Printf("vbcable: saving the install state: %v", err)
		}
		err = vbcable.RunSetup(setup)
		switch {
		case errors.Is(err, vbcable.ErrCancelled):
			logx.Printf("vbcable: UAC prompt declined")
			_ = vbcable.ClearPending(dataDir)
			_ = os.RemoveAll(vbcable.Dir(dataDir))
			a.tray.Post(func() {
				a.cable = cableInstall{err: "Installation cancelled."}
				a.render()
			})
			return
		case errors.Is(err, vbcable.ErrStillRunning):
			// It may still finish: keep the pending state and come back
			// after a restart, like a finished install.
			logx.Printf("vbcable: %v", err)
			a.armResume()
			a.tray.Post(func() {
				a.cable = cableInstall{state: cableRestart, err: "The installer is still running. Restart your PC once it's done."}
				a.render()
			})
			return
		case err != nil:
			_ = vbcable.ClearPending(dataDir)
			_ = os.RemoveAll(vbcable.Dir(dataDir))
			a.cableFailed(err)
			return
		}
		_ = os.RemoveAll(vbcable.Dir(dataDir)) // the driver is in Windows' store now

		a.tray.Post(func() { a.cable.state = cableChecking; a.render() })
		found := waitForCable(20 * time.Second)
		if found {
			time.Sleep(time.Second) // Windows may switch defaults right after the devices appear
		}
		restored := restoreDefaults(defaults)
		a.armResume() // taken back if the cable turns out to work
		logx.Printf("vbcable: setup done, cable found=%v", found)
		a.settleCable(found, restored)
	}()
}

// settleCable ends an install or wake: with the cable's devices up and a tone
// coming through, no restart is needed; otherwise the panel offers it. Off the
// UI thread.
func (a *App) settleCable(found bool, restored []string) {
	opened := make(chan string, 1)
	a.tray.Post(func() {
		a.refreshDevices(true)
		a.refreshRouting(true) // opens the automatic cable output
		a.render()
		far := ""
		if a.cableSelected() && a.route.CableOutput != nil {
			far = a.route.CableOutput.ID
		}
		opened <- far
	})
	far := <-opened
	works := found && far != "" && probeCable(a.engine, far)
	a.tray.Post(func() {
		note := restoredNote(restored)
		a.readCableHealth()
		if works && a.cableHealth == vbcable.HealthReady {
			a.cableDone(note)
			return
		}
		a.cable = cableInstall{state: cableRestart, note: note}
		a.render()
		if !a.tray.Popup().Visible() { // the UAC prompt took the focus and closed the panel
			go a.showNotice("VB-Cable is installed", "Restart your PC to finish. Resound reopens afterwards.", "Open panel")
		}
	})
}

// probeCable tests the cable with its quiet tone, twice if it stays silent:
// the driver can take a moment after its devices appear. A test that can't
// tell doesn't count as working: the restart stays on offer, optional.
func probeCable(engine *audio.Engine, far string) bool {
	for try := 0; ; try++ {
		result, err := engine.ProbeCable(far)
		logx.Printf("vbcable: test tone: result %d, %v", result, err)
		switch {
		case result == audio.ProbePassed:
			return true
		case result == audio.ProbeUnknown || try == 1:
			return false
		}
		time.Sleep(2 * time.Second)
	}
}

// cableDone finishes without a restart: nothing waits for one any more.
func (a *App) cableDone(note string) {
	_ = vbcable.ClearPending(config.Dir())
	if err := install.SetResumeOnce(false); err != nil {
		logx.Printf("vbcable: %v", err)
	}
	install.UnregisterRestart()
	logx.Printf("vbcable: working, no restart needed")
	a.cable = cableInstall{note: strings.TrimSpace("VB-Cable is ready. No restart needed. " + note)}
	a.render()
	if !a.tray.Popup().Visible() { // the UAC prompt took the focus and closed the panel
		go a.showNotice("VB-Cable is ready", "No restart needed. Open the panel to connect voice chat.", "Open panel")
	}
}

// wakeCable tries to bring up a cable that waits for a restart by restarting
// only its own device (Windows asks for approval). Nothing else is touched;
// if it doesn't help, the restart is still there.
func (a *App) wakeCable() {
	if a.cable.state != cableRestart {
		return
	}
	note, detected := a.cable.note, a.cable.detected
	a.cable = cableInstall{state: cableWaking, note: note, detected: detected}
	a.render()
	logx.Printf("vbcable: waking the device")
	go func() {
		err := vbcable.Wake()
		found := err == nil && waitForCable(20*time.Second)
		if !found {
			logx.Printf("vbcable: wake: %v, cable found=%v", err, found)
			a.tray.Post(func() {
				a.readCableHealth()
				a.cable = cableInstall{state: cableRestart, note: note, detected: detected,
					wakeFailed: !errors.Is(err, vbcable.ErrCancelled)}
				if a.cable.wakeFailed {
					a.cable.err = "That didn't help. Please restart your PC."
				}
				a.render()
			})
			return
		}
		time.Sleep(time.Second) // Windows may switch defaults right after the devices appear
		var restored []string
		if p, ok := vbcable.LoadPending(config.Dir()); ok {
			restored = restoreDefaults(p.Defaults)
		}
		a.settleCable(true, restored)
	}()
}

// readCableHealth asks Windows how the cable is.
func (a *App) readCableHealth() { a.cableHealth = vbcable.CheckHealth(a.route.CableInstalled()) }

// syncCableHealth notices a cable that waits for a restart without an install
// from here (one installed by hand, or just removed), so the panel says so
// instead of offering an install over it, and notices when that ended.
func (a *App) syncCableHealth() {
	if a.cable.busy() || a.ownRouting() {
		return
	}
	a.readCableHealth()
	wasDetected := a.cable.detected
	if !a.applyCableHealth() {
		return
	}
	dataDir := config.Dir()
	if a.cable.detected {
		logx.Printf("vbcable: found waiting for a restart (%d)", a.cableHealth)
		if _, ok := vbcable.LoadPending(dataDir); !ok {
			if err := vbcable.SavePending(dataDir, vbcable.Pending{Started: time.Now(), BootTime: vbcable.BootTime(), Detected: true}); err != nil {
				logx.Printf("vbcable: saving the install state: %v", err)
			}
		}
	} else if wasDetected {
		_ = vbcable.ClearPending(dataDir)
		_ = install.SetResumeOnce(false)
	}
}

// applyCableHealth updates the panel's state from a.cableHealth and reports
// whether it changed. After a restart that didn't help (cableStuck) a restart
// is not asked for again: the install is offered, with what went wrong.
func (a *App) applyCableHealth() bool {
	waits := a.cableHealth == vbcable.HealthPending || a.cableHealth == vbcable.HealthRemoved
	switch {
	case a.cableStuck:
		if !waits {
			a.cableStuck = false
		}
	case a.cable.state == cableIdle && waits:
		a.cable = cableInstall{state: cableRestart, detected: true}
		return true
	case a.cable.state == cableRestart && a.cable.detected && !waits:
		a.cable = cableInstall{}
		return true
	}
	return false
}

func (a *App) cableFailed(err error) {
	logx.Printf("vbcable: automatic install failed: %v", err)
	a.tray.Post(func() {
		a.cable = cableInstall{err: "Installation failed. Try again, or install it manually."}
		a.render()
	})
}

// waitForCable polls until both cable devices are active. Off the UI thread.
func waitForCable(timeout time.Duration) bool {
	for deadline := time.Now().Add(timeout); ; time.Sleep(time.Second) {
		if st, err := routing.Check("", ""); err == nil && vbcable.CheckHealth(st.CableInstalled()) == vbcable.HealthReady {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
	}
}

// restoreDefaults puts back defaults that Windows moved to the cable and
// returns the device names it restored. Off the UI thread.
func restoreDefaults(d vbcable.Defaults) []string {
	changed, err := vbcable.RestoreDefaults(d)
	if err != nil {
		logx.Printf("vbcable: restoring default devices: %v", err)
	}
	if len(changed) > 0 {
		logx.Printf("vbcable: Windows had made the cable the default; restored %q", changed)
	}
	return changed
}

// restoredNote is the short panel note; the device names are in the log.
func restoredNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "Your audio devices were kept."
}

// armResume makes Windows start Resound once after the restart.
func (a *App) armResume() {
	if err := install.SetResumeOnce(true); err != nil {
		logx.Printf("vbcable: registering the start after the restart: %v", err)
	}
}

func (a *App) cancelCableDownload() {
	if a.cable.state == cableDownloading && a.cable.cancel != nil {
		a.cable.cancel()
	}
}

// restartWindows saves everything, releases the mic and keys, and restarts.
func (a *App) restartWindows() {
	a.cable.confirm = false
	a.armResume()
	if err := install.RegisterRestart(); err != nil {
		logx.Printf("vbcable: %v (RunOnce still starts the app)", err)
	}
	a.stopAll()
	a.prepareForShutdown()
	logx.Printf("vbcable: restarting Windows")
	if err := install.RestartWindows(); err != nil {
		logx.Printf("restart: %v", err)
		a.cable.err = "Couldn't restart. Please restart your PC from the Start menu."
	}
	a.render()
}

// skipCableRestart continues with a cable that already works.
func (a *App) skipCableRestart() {
	_ = vbcable.ClearPending(config.Dir())
	if err := install.SetResumeOnce(false); err != nil {
		logx.Printf("vbcable: %v", err)
	}
	install.UnregisterRestart()
	logx.Printf("vbcable: continuing without a restart")
	a.cable = cableInstall{note: a.cable.note}
	a.render()
}

// prepareForShutdown leaves nothing behind when the process is about to end
// without Run returning (Windows restarting or signing out).
func (a *App) prepareForShutdown() {
	a.stopSoundsZip() // removes its unfinished file
	a.releasePlayback()
	a.releaseAllPTT()
	a.save()
	if err := a.lib.Flush(); err != nil {
		logx.Printf("flushing the sound index: %v", err)
	}
}

// resumeCableInstall continues an install recorded before this start. It
// runs during startup, after the device checks and before the outputs open
// (so a restored default is the one "System default" opens). It returns a
// notice to show, if any.
func (a *App) resumeCableInstall() (title, msg, confirm string) {
	dataDir := config.Dir()
	p, ok := vbcable.LoadPending(dataDir)
	if !ok {
		return "", "", ""
	}
	if p.Detected {
		// Only found here, so nothing to continue and nothing to tell.
		a.readCableHealth()
		if !p.Rebooted(vbcable.BootTime()) {
			a.cable = cableInstall{state: cableRestart, detected: true}
			return "", "", ""
		}
		logx.Printf("vbcable: restarted after finding the cable waiting")
		_ = vbcable.ClearPending(dataDir)
		_ = install.SetResumeOnce(false)
		if a.cableHealth != vbcable.HealthReady && a.cableHealth != vbcable.HealthMissing {
			a.cableStuck = true
			a.cable = cableInstall{err: "VB-Cable still isn't working after the restart. Try installing it again."}
		}
		return "", "", ""
	}
	a.returnTo(p)
	a.readCableHealth()
	if !p.Rebooted(vbcable.BootTime()) {
		// Only the app restarted: Windows still has to.
		logx.Printf("vbcable: install from %s still waits for a Windows restart", p.Started.Format(time.DateTime))
		a.cable = cableInstall{state: cableRestart}
		a.armResume()
		return "", "", ""
	}

	logx.Printf("vbcable: continuing after the restart (install from %s)", p.Started.Format(time.DateTime))
	restored := restoreDefaults(p.Defaults)
	if err := vbcable.ClearPending(dataDir); err != nil {
		logx.Printf("vbcable: %v", err)
	}
	// Started by the restart-apps registration or by the user: RunOnce
	// would otherwise start a second instance minutes later.
	_ = install.SetResumeOnce(false)
	if len(restored) > 0 {
		a.refreshDevices(true)
		a.refreshRouting(true)
	}
	if !a.route.CableInstalled() {
		a.cableStuck = true
		a.cable = cableInstall{err: "VB-Cable wasn't found. Try again, or install it manually."}
		return "VB-Cable wasn't found", "The installation didn't finish.", "Open panel"
	}
	a.cable = cableInstall{note: restoredNote(restored)}
	if p.ReturnTo == vbcable.ReturnWizard {
		return "Welcome back", "VB-Cable is ready. Let's finish the setup.", "Continue setup"
	}
	return "VB-Cable is ready", "Open the panel to connect voice chat.", "Open panel"
}

// returnTo opens the page the install was started from.
func (a *App) returnTo(p vbcable.Pending) {
	if p.ReturnTo == vbcable.ReturnWizard {
		// By name, not p.WizardStep: the setup installs VB-Cable only in
		// its audio step, whose number changed when steps were merged.
		a.page, a.wizardStep = pageWizard, wizardAudioStep
		return
	}
	a.page, a.parents = pageAudio, []page{pageSettings}
}

// --- rows --------------------------------------------------------------------

// cableStepInfo is step 1's status text while VB-Cable is missing.
func (a *App) cableStepInfo() string {
	switch a.cable.state {
	case cableDownloading, cableInstalling, cableChecking, cableWaking:
		return "Installing…"
	case cableRestart:
		return "Restart needed"
	}
	return "Missing"
}

// cableInstallRows offer the install while VB-Cable is missing, and show
// its progress.
func (a *App) cableInstallRows() []ui.Row {
	c := a.cable
	if c.busy() {
		return a.cableProgressRows()
	}
	if c.state == cableRestart {
		return a.cableRestartRows()
	}
	rows := []ui.Row{
		ui.Paragraph("Resound can install it for you. By installing it, you accept VB-Audio's license.", ui.ToneMuted),
		ui.Button("Install VB-Cable", ui.ButtonPrimary, false, a.installCable),
		ui.Text("Windows asks for your approval.", "", ui.ToneMuted),
	}
	if c.err != "" {
		rows = append(rows, ui.Paragraph(c.err, ui.ToneBad))
	}
	return append(rows, ui.Buttons(
		ui.Action{Label: "Install manually", Closes: true, OnClick: func() { _ = ui.ShellOpen(vbCableURL) }},
		ui.Action{Label: "Check again", OnClick: a.recheckCable},
	))
}

// cableProgressRows show what the install or wake is doing.
func (a *App) cableProgressRows() []ui.Row {
	c := a.cable
	switch c.state {
	case cableDownloading:
		pct := ""
		if c.percent >= 0 {
			pct = fmt.Sprintf("%d%%", c.percent)
		}
		return []ui.Row{
			ui.Text("Downloading VB-Cable…", pct, ui.ToneWarning),
			ui.Button("Cancel", ui.ButtonNormal, false, a.cancelCableDownload),
		}
	case cableInstalling:
		return []ui.Row{
			ui.Text("Installing VB-Cable…", "", ui.ToneWarning),
			ui.Text("Please confirm the Windows prompts.", "", ui.ToneMuted),
		}
	case cableWaking:
		return []ui.Row{
			ui.Text("Starting VB-Cable…", "", ui.ToneWarning),
			ui.Text("Please confirm the Windows prompt.", "", ui.ToneMuted),
		}
	}
	return []ui.Row{ui.Text("Checking VB-Cable…", "", ui.ToneWarning)}
}

// cableRestartRows offer the restart: needed while the driver waits for one
// or was just removed, optional once the cable's devices are up. While the
// driver's device itself waits, restarting just it may do.
func (a *App) cableRestartRows() []ui.Row {
	c := a.cable
	found := a.cableHealth == vbcable.HealthReady
	text := "Windows needs a restart to finish setting up VB-Cable. Resound reopens afterwards."
	switch {
	case found:
		text = "VB-Cable is installed, but Resound couldn't confirm it works yet. A restart finishes the setup; Resound reopens afterwards."
	case a.cableHealth == vbcable.HealthRemoved:
		text = "VB-Cable was removed. Windows needs a restart to finish. Resound reopens afterwards."
	}
	rows := []ui.Row{ui.Paragraph(text, ui.ToneWarning)}
	if c.note != "" {
		rows = append(rows, ui.Paragraph(c.note, ui.ToneGood))
	}
	if c.err != "" {
		rows = append(rows, ui.Paragraph(c.err, ui.ToneBad))
	}
	if c.confirm {
		return append(rows,
			ui.Paragraph("Save your work first. Your PC restarts right away.", ui.ToneWarning),
			ui.Buttons(
				ui.Action{Label: "Cancel", OnClick: func() { a.cable.confirm = false; a.render() }},
				ui.Action{Label: "Restart now", Icon: ui.IconPower, Style: ui.ButtonDanger, OnClick: a.restartWindows},
			))
	}
	restart := ui.Action{Label: "Restart now", Icon: ui.IconPower, Style: ui.ButtonPrimary, OnClick: func() { a.cable.confirm = true; a.render() }}
	if found {
		return append(rows, ui.Buttons(restart, ui.Action{Label: "Not now", OnClick: a.skipCableRestart}))
	}
	if a.cableHealth != vbcable.HealthPending || c.wakeFailed {
		return append(rows, ui.Buttons(restart))
	}
	return append(rows,
		ui.Buttons(restart, ui.Action{Label: "Try without restart", OnClick: a.wakeCable}),
		ui.Text("Windows asks for approval before trying.", "", ui.ToneMuted))
}

// cableRateRows warn while the cable's two ends run at different sample rates.
func (a *App) cableRateRows() []ui.Row {
	st := a.route
	if a.cable.busy() || !st.CableRatesDiffer() {
		return nil
	}
	return []ui.Row{ui.Warning("VB-Cable's sample rates differ: click to fix",
		fmt.Sprintf("CABLE Input runs at %.1f kHz and CABLE Output at %.1f kHz, so the sound is converted inside the cable. "+
			"Click to set both to %d kHz. Apps using the cable may reconnect for a moment.",
			float64(st.CableInputRate)/1000, float64(st.CableOutputRate)/1000, routing.CableRate/1000),
		a.fixCableRates)}
}

// fixCableRates puts both ends of the cable on the same rate. Streams on them
// end when the format changes, so the cable output opens again.
func (a *App) fixCableRates() {
	if err := routing.SetCableRates(a.route); err != nil {
		logx.Printf("vbcable: setting the sample rates: %v", err)
		a.routeErr = fmt.Errorf("couldn't change VB-Cable's sample rates: %w", err)
		a.render()
		return
	}
	logx.Printf("vbcable: both ends set to %d Hz", routing.CableRate)
	a.refreshRouting(true)
	if a.cableSelected() {
		a.openCableOutput()
	}
	a.closeLevels() // reopened by the flow, as they recorded the old format
	a.render()
}

// cableNoteRows show what a finished install did, once VB-Cable works.
func (a *App) cableNoteRows() []ui.Row {
	if a.cable.busy() {
		return a.cableProgressRows()
	}
	if a.cable.state == cableRestart {
		return a.cableRestartRows()
	}
	if a.cable.note != "" {
		return []ui.Row{ui.Paragraph(a.cable.note, ui.ToneGood)}
	}
	return nil
}

// cableCredit names VB-Cable's maker and asks for support, as VB-Audio's
// donationware terms expect from apps that install it.
func (a *App) cableCredit() []ui.Row {
	return []ui.Row{
		ui.Paragraph("VB-Cable is donationware by VB-Audio. If you like it, consider supporting them.", ui.ToneMuted),
		ui.Buttons(
			ui.Action{Label: "vb-cable.com", Icon: ui.IconExternalLink, Closes: true, OnClick: func() { _ = ui.ShellOpen(vbCableURL) }},
			ui.Action{Label: "Donate", Icon: ui.IconExternalLink, Closes: true, OnClick: func() { _ = ui.ShellOpen(vbDonateURL) }},
		),
	}
}
