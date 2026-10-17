package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/install"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
	"github.com/motivated2die/resound/internal/ui"
	"github.com/motivated2die/resound/internal/vbcable"
)

// Installing VB-Cable for the user (see package vbcable): download the
// official pack, run its setup silently with administrator approval, put
// back default devices Windows moved to the cable, then offer the restart
// the driver needs. A RunOnce entry and the pending-install file bring the
// app back after the restart, on the page the install started from.

const vbDonateURL = "https://vb-audio.com/Services/licensing.htm"

type cableState int

const (
	cableIdle        cableState = iota
	cableDownloading            // fetching and checking the driver pack
	cableInstalling             // the setup runs (UAC and driver prompts)
	cableChecking               // waiting for the devices to appear
	cableRestart                // installed; a restart is needed or recommended
)

type cableInstall struct {
	state   cableState
	percent int    // download progress, -1 while unknown
	err     string // why the last attempt failed, shown with the manual way
	note    string // what the install did, e.g. restored default devices
	confirm bool   // the restart confirmation is shown
	cancel  context.CancelFunc
}

func (c cableInstall) busy() bool {
	return c.state == cableDownloading || c.state == cableInstalling || c.state == cableChecking
}

// installCable starts the automatic install.
func (a *App) installCable() {
	if a.cable.state != cableIdle {
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
		a.armResume()
		logx.Printf("vbcable: setup done, cable found=%v", found)

		a.tray.Post(func() {
			a.cable = cableInstall{state: cableRestart, note: restoredNote(restored)}
			a.refreshDevices(true)
			a.refreshRouting(true)
			a.render()
			if !a.tray.Popup().Visible() { // the UAC prompt took the focus and closed the panel
				go a.showNotice("VB-Cable is installed", "Restart your PC to finish. Resound reopens afterwards.", "Open panel")
			}
		})
	}()
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
		if st, err := routing.Check("", ""); err == nil && st.CableInstalled() {
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
	a.returnTo(p)
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
	case cableDownloading, cableInstalling, cableChecking:
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
	case cableChecking:
		return []ui.Row{ui.Text("Finishing up…", "", ui.ToneWarning)}
	case cableRestart:
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

// cableRestartRows offer the restart after an install: needed while the
// cable isn't visible yet, recommended by VB-Audio once it is.
func (a *App) cableRestartRows() []ui.Row {
	c := a.cable
	found := a.route.CableInstalled()
	text := "Restart your PC to finish. Resound reopens afterwards."
	if found {
		text = "VB-Cable is installed. Restart your PC to finish; Resound reopens afterwards."
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
	if !found {
		return append(rows, ui.Buttons(restart))
	}
	return append(rows, ui.Buttons(restart, ui.Action{Label: "Not now", OnClick: a.skipCableRestart}))
}

// cableNoteRows show what a finished install did, once VB-Cable works.
func (a *App) cableNoteRows() []ui.Row {
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
