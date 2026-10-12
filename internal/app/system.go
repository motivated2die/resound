package app

import (
	"errors"

	"github.com/motivated2die/resound/internal/elevate"
	"github.com/motivated2die/resound/internal/install"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

func (a *App) appSummary() (string, ui.Tone) {
	if a.update.latest != nil {
		return "v" + Version + " · update to v" + a.update.latest.Version, ui.ToneWarning
	}
	if a.portable {
		return "v" + Version + " · portable", ui.ToneMuted
	}
	return "v" + Version, ui.ToneGood
}

func (a *App) appPage() []ui.Row {
	mode := "Installed"
	if a.portable {
		mode = "Portable (dev build)"
	}
	rows := []ui.Row{
		ui.Header("App & startup", a.back),
		ui.Text("Version", Version, ui.ToneNormal),
		ui.Text("Mode", mode, ui.ToneNormal),
		ui.Toggle("Start with Windows", install.AutoStartEnabled(), a.toggleAutoStart),
	}
	rows = append(rows, a.updateRows()...)
	rows = append(rows, ui.Separator())
	if a.portable {
		return append(rows,
			ui.Text("Development builds never install themselves.", "", ui.ToneMuted),
			ui.Text("Run a release build from outside the repo to install.", "", ui.ToneMuted),
			ui.Spacer(4))
	}
	return append(rows,
		ui.ClosingButton("Uninstall Resound…", ui.ButtonDanger, a.uninstall),
		ui.Spacer(4))
}

func (a *App) toggleAutoStart() {
	on := !install.AutoStartEnabled()
	if err := install.SetAutoStart(on); err != nil {
		logx.Printf("autostart: %v", err)
	} else {
		logx.Printf("autostart: %v", on)
	}
	a.render()
}

// uninstall starts the installed exe with --uninstall; that process asks
// for confirmation and, if confirmed, tells this instance to quit.
func (a *App) uninstall() {
	exe, err := install.ExePath()
	if err == nil {
		err = ui.ShellRun(exe, install.UninstallCommand)
	}
	if err != nil {
		logx.Printf("uninstall: %v", err)
	}
}

// allowFirewall adds the firewall rule through the elevated helper.
func (a *App) allowFirewall() {
	if a.fwBusy {
		return
	}
	a.fwBusy, a.fwResult = true, ""
	a.render()
	go func() {
		err := install.AllowFirewall()
		a.tray.Post(func() {
			a.fwBusy = false
			switch {
			case errors.Is(err, elevate.ErrCancelled):
				a.fwResult, a.fwFailed = "", false
			case err != nil:
				logx.Printf("firewall: %v", err)
				a.fwResult, a.fwFailed = "Could not change the firewall (see log).", true
			default:
				logx.Printf("firewall: rule added")
				a.fwResult, a.fwFailed = "Phones are allowed through the firewall.", false
			}
			a.render()
		})
	}()
}
