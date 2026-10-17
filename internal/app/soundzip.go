package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/backup"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// Backup & restore → Sound files: a zip of all library folders, their
// subfolders (empty ones too) and their sounds, as the library scanned them
// (backup.ZipSounds). It can be large, so the page says how large first,
// and it is saved only where the user chooses, never in the data folder.
// Resound doesn't restore it: it is for keeping or moving the sounds.

type soundsZip struct {
	confirm bool // the size warning is shown
	running bool
	percent int
	cancel  context.CancelFunc
	done    chan struct{} // closed when the running zip has ended
	note    string        // result of the last zip, kept until the next one
	failed  bool
	path    string // the last zip saved, for "Show in folder"
}

var zipFileFilter = []ui.FileFilter{{Name: "Zip files (*.zip)", Pattern: "*.zip"}, {Name: "All files", Pattern: "*.*"}}

// soundFolders lists the library folders with their subfolders and sound
// files as the last scan found them, with the files' count and total size.
func (a *App) soundFolders() (folders []backup.SoundFolder, files int, size int64) {
	tabs := a.lib.Tabs()
	for _, root := range a.cfg.Folders {
		f := backup.SoundFolder{Root: root}
		for _, sub := range a.lib.Folders(root) {
			f.Folders = append(f.Folders, sub.Path)
		}
		for _, t := range tabs {
			if !strings.EqualFold(filepath.Clean(t.Root), filepath.Clean(root)) {
				continue
			}
			for _, s := range t.Sounds {
				f.Files = append(f.Files, s.Path)
				files++
				size += s.Size
			}
		}
		folders = append(folders, f)
	}
	return folders, files, size
}

// soundsZipRows are the Sound files section of the Backup & restore page.
func (a *App) soundsZipRows() []ui.Row {
	z := &a.zip
	rows := []ui.Row{
		ui.Section("Sound files"),
		ui.Paragraph("Zip all your sound folders with their subfolders into one file, to keep or move them. "+
			"To use it again, unzip it and add the folders under Sound library.", ui.ToneMuted),
	}
	switch {
	case len(a.cfg.Folders) == 0:
		rows = append(rows, ui.Text("No sound folders yet.", "", ui.ToneMuted))
	case z.running:
		rows = append(rows,
			ui.Text("Zipping your sounds…", fmt.Sprintf("%d%%", z.percent), ui.ToneGood),
			ui.Button("Cancel", ui.ButtonNormal, false, a.cancelSoundsZip))
	case z.confirm:
		_, files, size := a.soundFolders()
		rows = append(rows,
			ui.Paragraph(fmt.Sprintf("This makes a zipped copy of all your sounds: %s in %s, about %s. "+
				"It needs that much free space where you save it, and large libraries take a while.",
				plural(files, "sound"), plural(len(a.cfg.Folders), "library folder"), audio.FormatBytes(size)), ui.ToneWarning),
			ui.Buttons(
				ui.Action{Label: "Cancel", OnClick: func() { z.confirm = false; a.render() }},
				ui.Action{Label: "Save as…", Icon: ui.IconSave, Style: ui.ButtonPrimary, Closes: true, OnClick: a.zipSoundsSaveAs},
			))
	default:
		rows = append(rows, ui.Buttons(ui.Action{Label: "Zip all sounds…", Icon: ui.IconFolder, OnClick: a.confirmSoundsZip}))
	}
	if z.note != "" && !z.running {
		tone := ui.ToneGood
		if z.failed {
			tone = ui.ToneBad
		}
		rows = append(rows, ui.Paragraph(z.note, tone))
		if !z.failed && z.path != "" {
			path := z.path
			rows = append(rows, ui.Buttons(ui.Action{Label: "Show in folder", Icon: ui.IconExternalLink, Closes: true,
				OnClick: func() { _ = ui.ShellRun("explorer.exe", `/select,"`+path+`"`) }}))
		}
	}
	return rows
}

// confirmSoundsZip shows how large the zip gets, from a fresh scan.
func (a *App) confirmSoundsZip() {
	a.lib.Rescan(5 * time.Second)
	a.zip.confirm, a.zip.note = true, ""
	a.render()
}

// zipSoundsSaveAs asks where to save the zip, then starts it.
func (a *App) zipSoundsSaveAs() {
	defer a.reopenPanel()
	a.zip.confirm = false
	// No folder given: the dialog starts where the user last saved (or in
	// Documents), not in Resound's data folder.
	path, ok, err := ui.SaveFileDialog(a.tray.Hwnd(), "Save a zip of your sounds", "", backup.SoundsName(time.Now()), "zip", zipFileFilter)
	if err != nil {
		logx.Printf("sounds zip: save dialog: %v", err)
	}
	if !ok {
		return
	}
	a.lib.Rescan(5 * time.Second)
	folders, files, size := a.soundFolders()
	if free, err := freeSpace(filepath.Dir(path)); err == nil && free < uint64(size)+1<<20 {
		a.zip.note, a.zip.failed = fmt.Sprintf("Not enough space: the zip needs about %s, but only %s are free there. Choose another drive.",
			audio.FormatBytes(size), audio.FormatBytes(int64(free))), true
		return
	}
	a.startSoundsZip(path, folders, files, size)
}

// startSoundsZip writes the zip on its own goroutine, posting its progress.
func (a *App) startSoundsZip(path string, folders []backup.SoundFolder, files int, size int64) {
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	a.zip = soundsZip{running: true, cancel: cancel, done: finished}
	logx.Printf("sounds zip: zipping %d sound(s), %s, from %d folder(s) to %s", files, audio.FormatBytes(size), len(folders), path)
	go func() {
		defer close(finished)
		defer cancel()
		lastPct, lastPost := -1, time.Time{}
		res, err := backup.ZipSounds(ctx, path, folders, func(done, total int64) {
			pct := int(done * 100 / max(1, total))
			if pct == lastPct || (pct < 100 && time.Since(lastPost) < 150*time.Millisecond) {
				return
			}
			lastPct, lastPost = pct, time.Now()
			a.tray.Post(func() {
				if a.zip.running {
					a.zip.percent = pct
					a.render()
				}
			})
		})
		cancelled := err != nil && ctx.Err() != nil // read now: the deferred cancel runs before the UI thread does
		a.tray.Post(func() { a.finishSoundsZip(res, err, cancelled) })
	}()
}

func (a *App) finishSoundsZip(res backup.SoundsResult, err error, cancelled bool) {
	z := &a.zip
	z.running, z.cancel = false, nil
	switch {
	case cancelled:
		logx.Printf("sounds zip: cancelled")
		z.note, z.failed = "Zipping cancelled. Nothing was saved.", false
	case err != nil:
		logx.Printf("sounds zip: %v", err)
		z.note, z.failed = "Zipping failed: "+err.Error(), true
	default:
		logx.Printf("sounds zip: saved %s (%s, %d sound(s), %d skipped)", res.Path, audio.FormatBytes(res.Size), res.Files, res.Skipped)
		z.note = fmt.Sprintf("Saved “%s” (%s, %s).", filepath.Base(res.Path), audio.FormatBytes(res.Size), plural(res.Files, "sound"))
		if res.Skipped > 0 {
			z.note += fmt.Sprintf(" %s couldn't be read and are missing from it.", plural(res.Skipped, "file"))
		}
		z.failed, z.path = false, res.Path
	}
	a.render()
}

func (a *App) cancelSoundsZip() {
	if a.zip.cancel != nil {
		a.zip.cancel()
	}
}

// stopSoundsZip cancels a running zip and waits a moment for it to remove
// its unfinished file, before the app exits.
func (a *App) stopSoundsZip() {
	if a.zip.cancel == nil {
		return
	}
	a.zip.cancel()
	select {
	case <-a.zip.done:
	case <-time.After(3 * time.Second):
	}
}

// freeSpace returns the bytes free for this user on the drive of dir.
func freeSpace(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	err = windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree)
	return free, err
}
