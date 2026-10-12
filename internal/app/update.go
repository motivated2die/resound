package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
	"github.com/motivated2die/resound/internal/update"
)

// Updates (see package update): shortly after start and then twice a day,
// the app asks GitHub for the latest full release. A newer one shows a dot
// next to the logo; "Update now" on the App & startup page downloads the
// exe, checks it and starts it. That copy installs itself like any
// downloaded exe (install.Ensure): it asks this instance to quit, replaces
// the installed exe and starts it with the "updated" notice.

const (
	updateFirstCheck = 10 * time.Second
	updateInterval   = 12 * time.Hour

	// updateAPIEnv points the check at another API base, for testing with
	// cmd/devupdate: https, or http to localhost only.
	updateAPIEnv = "RESOUND_UPDATE_API"
)

type updateState int

const (
	updateIdle        updateState = iota
	updateChecking                // asking GitHub
	updateDownloading             // fetching and checking the exe
	updateStarting                // the new exe runs and will replace this one
)

type updater struct {
	client  update.Client
	state   updateState
	latest  *update.Release // a newer release than this one, nil if none
	checked time.Time       // last successful check
	err     string          // last failed check or update, shown on the App page
	percent int             // download progress, -1 while unknown
	cancel  context.CancelFunc
}

func updateDir() string { return filepath.Join(config.Dir(), "update") }

// startUpdateChecks runs the periodic check for the lifetime of the app.
func (a *App) startUpdateChecks() {
	a.update.client = update.Client{UserAgent: "Resound/" + Version}
	if api := os.Getenv(updateAPIEnv); api != "" {
		if err := update.CheckAPI(api); err != nil {
			logx.Printf("update: ignoring %s: %v", updateAPIEnv, err)
		} else {
			a.update.client.API = api
			logx.Printf("update: using the test API %s", api)
		}
	}
	go func() {
		time.Sleep(updateFirstCheck)
		// What an earlier update downloaded; its installer has long exited.
		_ = os.RemoveAll(updateDir())
		for {
			a.tray.Post(func() {
				if !a.cfg.NoUpdateCheck {
					a.checkForUpdate()
				}
			})
			time.Sleep(updateInterval)
		}
	}()
}

// checkForUpdate asks GitHub for the latest release, on its own goroutine.
func (a *App) checkForUpdate() {
	if a.update.state != updateIdle {
		return
	}
	a.update.state = updateChecking
	a.render()
	client := a.update.client
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		rel, err := client.Latest(ctx)
		a.tray.Post(func() {
			a.update.state = updateIdle
			switch {
			case errors.Is(err, update.ErrNoRelease):
				logx.Printf("update: no published release yet (%v)", err)
				a.update.latest, a.update.err, a.update.checked = nil, "", time.Now()
			case err != nil:
				logx.Printf("update: check failed: %v", err)
				a.update.err = "Could not check for updates: " + err.Error()
			case update.Newer(rel.Version, Version):
				if a.update.latest == nil || a.update.latest.Version != rel.Version {
					logx.Printf("update: v%s is available (running v%s)", rel.Version, Version)
				}
				a.update.latest, a.update.err, a.update.checked = &rel, "", time.Now()
			default:
				logx.Printf("update: up to date (latest release v%s, running v%s)", rel.Version, Version)
				a.update.latest, a.update.err, a.update.checked = nil, "", time.Now()
			}
			a.render()
		})
	}()
}

// installUpdate downloads the newer release and starts it.
func (a *App) installUpdate() {
	if a.update.state != updateIdle || a.update.latest == nil || a.portable {
		return
	}
	rel := *a.update.latest
	ctx, cancel := context.WithCancel(context.Background())
	a.update.state, a.update.percent, a.update.err, a.update.cancel = updateDownloading, -1, "", cancel
	a.render()
	logx.Printf("update: downloading v%s", rel.Version)
	client := a.update.client
	go func() {
		defer cancel()
		lastPct, lastPost := -1, time.Time{}
		exe, err := client.Download(ctx, rel, updateDir(), func(done, total int64) {
			if total <= 0 {
				return
			}
			pct := int(done * 100 / total)
			if pct == lastPct || (pct < 100 && time.Since(lastPost) < 150*time.Millisecond) {
				return
			}
			lastPct, lastPost = pct, time.Now()
			a.tray.Post(func() {
				if a.update.state == updateDownloading {
					a.update.percent = pct
					a.render()
				}
			})
		})
		if err == nil {
			logx.Printf("update: v%s downloaded and checked; starting %s", rel.Version, exe)
			// The new copy asks this instance to quit (OnQuitRequest),
			// which releases the mic and keys and saves everything.
			err = exec.Command(exe).Start()
		}
		cancelled := err != nil && ctx.Err() != nil // read now: the deferred cancel runs before the UI thread does
		a.tray.Post(func() {
			a.update.cancel = nil
			switch {
			case cancelled:
				logx.Printf("update: cancelled")
				a.update.state = updateIdle
			case err != nil:
				logx.Printf("update: %v", err)
				a.update.state, a.update.err = updateIdle, "The update failed: "+err.Error()
			default:
				a.update.state = updateStarting
				go a.watchUpdateStart()
			}
			a.render()
		})
	}()
}

// watchUpdateStart gives the started exe time to make this instance quit;
// if it hasn't, the install went wrong and the user can try again.
func (a *App) watchUpdateStart() {
	time.Sleep(time.Minute)
	a.tray.Post(func() {
		if a.update.state == updateStarting {
			logx.Printf("update: the new version did not take over within a minute")
			a.update.state, a.update.err = updateIdle, "The new version did not start. See the log in Diagnostics."
			a.render()
		}
	})
}

func (a *App) cancelUpdate() {
	if a.update.cancel != nil {
		a.update.cancel()
	}
}

func (a *App) toggleUpdateCheck() {
	a.cfg.NoUpdateCheck = !a.cfg.NoUpdateCheck
	a.save()
	if !a.cfg.NoUpdateCheck {
		a.checkForUpdate()
	}
	a.render()
}

// brandUpdate is the update badge in the home page header, nil when there
// is nothing to show.
func (a *App) brandUpdate() *ui.BrandUpdate {
	open := func() { a.openPage(pageApp) }
	switch {
	case a.update.state == updateDownloading || a.update.state == updateStarting:
		return &ui.BrandUpdate{Label: "Updating…", Tip: "Updating to v" + a.update.latest.Version, OnClick: open}
	case a.update.latest != nil:
		return &ui.BrandUpdate{Label: "Update", Tip: "Resound v" + a.update.latest.Version + " is available", OnClick: open}
	}
	return nil
}

// updateRows are the Updates section of the App & startup page.
func (a *App) updateRows() []ui.Row {
	u := &a.update
	rows := []ui.Row{
		ui.Section("Updates"),
		ui.Toggle("Check for updates automatically", !a.cfg.NoUpdateCheck, a.toggleUpdateCheck),
	}
	page := update.ReleasesPage
	switch {
	case u.state == updateDownloading:
		progress := "…"
		if u.percent >= 0 {
			progress = fmt.Sprintf("%d%%", u.percent)
		}
		rows = append(rows,
			ui.Text("Downloading v"+u.latest.Version, progress, ui.ToneGood),
			ui.Button("Cancel", ui.ButtonNormal, false, a.cancelUpdate))
	case u.state == updateStarting:
		rows = append(rows, ui.Paragraph("Installing v"+u.latest.Version+". Resound restarts in a moment.", ui.ToneGood))
	case u.latest != nil:
		page = u.latest.Page
		rows = append(rows, ui.Text("New version", "v"+u.latest.Version, ui.ToneGood))
		if a.portable {
			rows = append(rows, ui.Paragraph("Development builds don't update themselves. Install a release build to update.", ui.ToneMuted))
		} else {
			rows = append(rows, ui.Buttons(
				ui.Action{Label: "Update now", Icon: ui.IconRefresh, Style: ui.ButtonPrimary, OnClick: a.installUpdate},
				ui.Action{Label: "What's new", Icon: ui.IconExternalLink, Closes: true, OnClick: func() { _ = ui.ShellOpen(page) }},
			))
		}
	case u.state == updateChecking:
		rows = append(rows, ui.Text("Checking for updates…", "", ui.ToneMuted))
	case !u.checked.IsZero():
		rows = append(rows, ui.Text("Up to date", "checked "+u.checked.Format("15:04"), ui.ToneMuted))
	}
	if u.err != "" {
		rows = append(rows, ui.Paragraph(u.err, ui.ToneBad))
	}
	if u.state == updateIdle {
		rows = append(rows, ui.Button("Check now", ui.ButtonNormal, false, a.checkForUpdate))
	}
	return rows
}
