package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/backup"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// Restoring replaces files the running app rewrites when it exits (config,
// sound index), so it can't happen in place. The app starts a new instance
// with restoreFlag and waitFlag and quits; the new instance waits for it to
// exit, saves the current state as a safety backup, restores, and then
// starts normally with the restored settings.
const (
	restoreFlag = "--restore="
	waitFlag    = "--wait-for="
)

var zipFilter = []ui.FileFilter{{Name: "Resound backups (*.zip)", Pattern: "*.zip"}, {Name: "All files", Pattern: "*.*"}}

func backupTime(t time.Time) string { return t.Local().Format("Mon 2 Jan 2006, 15:04") }

func (a *App) backupSummary() (string, ui.Tone) {
	list := backup.List(config.Dir())
	if len(list) == 0 {
		return "No backups", ui.ToneMuted
	}
	return "Last " + list[0].Created.Local().Format("2 Jan, 15:04"), ui.ToneGood
}

func (a *App) backupPage() []ui.Row {
	dir := backup.Dir(config.Dir())
	rows := []ui.Row{
		ui.Header("Backup & restore", func() { a.restoreCandidate = ""; a.back() }),
		ui.Paragraph("A backup holds your settings: audio devices and volumes, sound folders, push-to-talk, "+
			"favorites, sound volumes and colors, phone layouts, and the PIN with paired phones. "+
			"Sound files are not included.", ui.ToneMuted),
		ui.Buttons(
			ui.Action{Label: "Back up now", Icon: ui.IconSave, Style: ui.ButtonPrimary, OnClick: a.backupNow},
			ui.Action{Label: "Save as…", Closes: true, OnClick: a.backupSaveAs},
		),
	}
	if a.backupNote != "" {
		tone := ui.ToneGood
		if a.backupFailed {
			tone = ui.ToneBad
		}
		rows = append(rows, ui.Paragraph(a.backupNote, tone))
	}

	list := backup.List(config.Dir())
	listed := false
	rows = append(rows, ui.Section("Backups"))
	if len(list) == 0 {
		rows = append(rows, ui.Text("No backups yet.", "", ui.ToneMuted))
	}
	for _, e := range list {
		path := e.Path
		where := filepath.Dir(path)
		if strings.EqualFold(filepath.Clean(where), filepath.Clean(dir)) {
			where = "backup folder"
		}
		detail := fmt.Sprintf("v%s · %s · %s", e.Version, audio.FormatBytes(e.Size), where)
		rows = append(rows, ui.Item(backupTime(e.Created), detail, "Restore", func() { a.restoreCandidate = path; a.render() }))
		if strings.EqualFold(a.restoreCandidate, path) {
			rows = append(rows, a.restoreConfirmRows(e.Created)...)
			listed = true
		}
	}
	rows = append(rows, ui.Spacer(4), ui.Buttons(
		ui.Action{Label: "Restore from file…", Icon: ui.IconRestore, Closes: true, OnClick: a.chooseRestoreFile},
		ui.Action{Label: "Open folder", Closes: true, OnClick: func() { _ = os.MkdirAll(dir, 0o755); _ = ui.ShellOpen(dir) }},
	))
	if a.restoreCandidate != "" && !listed { // a file picked from elsewhere
		m, _, err := backup.Read(a.restoreCandidate)
		if err == nil {
			rows = append(rows, ui.Text(filepath.Base(a.restoreCandidate), "", ui.ToneNormal))
			rows = append(rows, a.restoreConfirmRows(m.Created)...)
		}
	}
	return append(rows,
		ui.Paragraph("Backups are kept next to the settings. Uninstalling removes them, so save important ones elsewhere with “Save as…”.", ui.ToneMuted),
		ui.Spacer(4))
}

func (a *App) restoreConfirmRows(created time.Time) []ui.Row {
	path := a.restoreCandidate
	return []ui.Row{
		ui.Paragraph("Restore the backup from "+backupTime(created)+"? Resound restarts with those settings. "+
			"Your current settings are saved as a backup first.", ui.ToneWarning),
		ui.Buttons(
			ui.Action{Label: "Cancel", OnClick: func() { a.restoreCandidate = ""; a.render() }},
			ui.Action{Label: "Restore", Style: ui.ButtonDanger, OnClick: func() { a.restoreBackup(path) }},
		),
		ui.Separator(),
	}
}

// flushState writes pending settings so the backup has the current state.
func (a *App) flushState() {
	a.save()
	if err := a.lib.Flush(); err != nil {
		logx.Printf("backup: flushing the sound index: %v", err)
	}
}

// backupNow saves a backup named after the time into the backup folder.
func (a *App) backupNow() {
	a.flushState()
	now := time.Now()
	a.reportBackup(backup.Create(config.Dir(), filepath.Join(backup.Dir(config.Dir()), backup.DefaultName(now, "")), Version, now))
	a.render()
}

// backupSaveAs asks where to save the backup (the backup folder by default).
func (a *App) backupSaveAs() {
	defer a.reopenPanel()
	dir := backup.Dir(config.Dir())
	_ = os.MkdirAll(dir, 0o755)
	now := time.Now()
	path, ok, err := ui.SaveFileDialog(a.tray.Hwnd(), "Save a Resound backup", dir, backup.DefaultName(now, ""), "zip", zipFilter)
	if err != nil {
		logx.Printf("backup: save dialog: %v", err)
	}
	if !ok {
		return
	}
	a.flushState()
	a.reportBackup(backup.Create(config.Dir(), path, Version, now))
}

func (a *App) reportBackup(e backup.Entry, err error) {
	if err != nil {
		logx.Printf("backup: %v", err)
		a.backupNote, a.backupFailed = "The backup failed: "+err.Error(), true
		return
	}
	logx.Printf("backup: saved %s (%s)", e.Path, audio.FormatBytes(e.Size))
	a.backupNote, a.backupFailed = "Saved “"+filepath.Base(e.Path)+"”.", false
}

// chooseRestoreFile picks a backup from anywhere, e.g. on a fresh install.
func (a *App) chooseRestoreFile() {
	defer a.reopenPanel()
	path, ok, err := ui.OpenFileDialog(a.tray.Hwnd(), "Restore a Resound backup", zipFilter)
	if err != nil {
		logx.Printf("backup: open dialog: %v", err)
	}
	if !ok {
		return
	}
	if _, _, err := backup.Read(path); err != nil {
		a.restoreCandidate = ""
		a.backupNote, a.backupFailed = filepath.Base(path)+": "+err.Error(), true
		return
	}
	a.restoreCandidate, a.backupNote = path, ""
}

// restoreBackup hands the restore to a new instance and quits (see restoreFlag).
func (a *App) restoreBackup(path string) {
	if _, _, err := backup.Read(path); err != nil {
		a.backupNote, a.backupFailed = err.Error(), true
		a.render()
		return
	}
	exe, err := os.Executable()
	if err == nil {
		err = exec.Command(exe, restoreFlag+path, waitFlag+strconv.Itoa(os.Getpid())).Start()
	}
	if err != nil {
		logx.Printf("backup: starting the restore: %v", err)
		a.backupNote, a.backupFailed = "Could not restart Resound: "+err.Error(), true
		a.render()
		return
	}
	logx.Printf("backup: restoring %s in a new instance", path)
	a.quit()
}

// applyPendingRestore runs at startup, before the single-instance check.
// It returns a note for the user when a restore was requested.
func applyPendingRestore(args []string) (title, note string) {
	var path string
	var pid int
	for _, arg := range args {
		if v, ok := strings.CutPrefix(arg, restoreFlag); ok {
			path = v
		} else if v, ok := strings.CutPrefix(arg, waitFlag); ok {
			pid, _ = strconv.Atoi(v)
		}
	}
	if path == "" {
		return "", ""
	}
	if pid > 0 {
		waitForProcess(uint32(pid), 15*time.Second)
	}
	dataDir := config.Dir()
	now := time.Now()
	safety := filepath.Join(backup.Dir(dataDir), backup.DefaultName(now, "before-restore"))
	if _, err := backup.Create(dataDir, safety, Version, now); err != nil {
		logx.Printf("backup: safety backup before restoring: %v", err)
	}
	m, err := backup.Restore(dataDir, path)
	if err != nil {
		logx.Printf("backup: restoring %s: %v", path, err)
		return "Restore failed", "The backup could not be restored: " + err.Error() +
			" Your previous settings are listed under Settings → Backup & restore."
	}
	logx.Printf("backup: restored %s (made by v%s on %s)", path, m.Version, m.Created.Format(time.RFC3339))
	return "Settings restored", "Resound now uses the settings from the backup of " + backupTime(m.Created) + "."
}

// waitForProcess waits until the process exits, at most timeout.
func waitForProcess(pid uint32, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return // already gone
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
}
