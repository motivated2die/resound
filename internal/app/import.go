package app

// Importing sounds by drag and drop onto a folder on the Sound library page:
// a Myinstants sound (its name or link icon), any link to a sound file or a
// page with one sound, or sound files from Explorer. Links are downloaded
// in the background (package importer); the new sound is saved under its
// page's name, which the user can change right away in a text field.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/importer"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// myinstantsURL opens in the browser from the library page.
const myinstantsURL = "https://www.myinstants.com/"

const maxImportsShown = 6

type importState int

const (
	importRunning importState = iota
	importDone
	importFailed
)

// soundImport is one dropped link, from download to the saved file.
type soundImport struct {
	id    int
	dir   string // the folder it goes into
	host  string // where it comes from, for "Downloading from …"
	state importState
	err   string // failed: what to tell the user; done: a rename problem

	path, name, ext string // done: the saved file
	renaming        bool   // done: the name field is shown
}

// importer state, on App (UI thread).
type importsState struct {
	jobs    []*soundImport
	seq     int
	fetcher *importer.Fetcher
	ctx     context.Context
	cancel  context.CancelFunc
	warn    string // a drop that went nowhere, shown at the top of the page
	subdirs map[string][]string
}

func (s *importsState) init() {
	s.fetcher = importer.New(audio.SupportedExtensions, audio.CheckFile)
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

// clearFinished forgets finished imports and notes (the panel reopened);
// downloads still running stay.
func (s *importsState) clearFinished() {
	s.jobs = slices.DeleteFunc(s.jobs, func(j *soundImport) bool { return j.state != importRunning })
	s.warn = ""
}

// onDropAnywhere handles drops on the library page (or the setup's sounds
// step) outside the folder fields: folders and sound files add library
// folders as before; links need a folder to go into.
func (a *App) onDropAnywhere(d ui.DropData) {
	if !a.acceptsDrops() {
		return
	}
	a.imports.warn = ""
	if len(d.Paths) > 0 {
		a.addFolders(d.Paths)
		return
	}
	if len(d.Links) > 0 {
		if len(a.cfg.Folders) == 0 {
			a.imports.warn = "Add a sound folder first, then drop sounds onto it."
		} else {
			a.imports.warn = "Drop the sound onto one of the folders below to download it there."
		}
		a.render()
	}
}

// dropOnFolder returns the drop handler of the field for folder dir.
func (a *App) dropOnFolder(dir string) func(ui.DropData) {
	return func(d ui.DropData) {
		a.imports.warn = ""
		for _, link := range d.Links {
			a.importLink(dir, link)
		}
		var folders, sounds []string
		skipped := 0
		for _, p := range d.Paths {
			info, err := os.Stat(p)
			switch {
			case err != nil:
				skipped++
			case info.IsDir():
				folders = append(folders, p)
			case slices.Contains(audio.SupportedExtensions, strings.ToLower(filepath.Ext(p))):
				sounds = append(sounds, p)
			default:
				skipped++
			}
		}
		if len(folders) > 0 {
			a.addFolders(folders) // a folder dropped here becomes a library folder, as anywhere
		}
		if len(sounds) > 0 {
			a.copySounds(dir, sounds, skipped)
		} else if skipped > 0 && len(folders) == 0 {
			a.imports.warn = "Only MP3, WAV, FLAC and OGG files can be added."
		}
		a.render()
	}
}

// importLink starts downloading a dropped link into dir.
func (a *App) importLink(dir, link string) {
	s := &a.imports
	u, ok := importer.ParseLink(link)
	if !ok {
		a.imports.warn = "That is not a web link to a sound."
		return
	}
	s.seq++
	job := &soundImport{id: s.seq, dir: dir, host: u.Hostname()}
	s.jobs = append(s.jobs, job)
	if n := len(s.jobs); n > maxImportsShown {
		s.jobs = slices.Delete(s.jobs, 0, n-maxImportsShown)
	}
	logx.Printf("import: downloading %s into %s", redactQuery(u), dir)
	go func() {
		saved, err := s.fetcher.Fetch(s.ctx, u.String(), dir)
		a.tray.Post(func() { a.finishImport(job, saved, err) })
	}()
}

// redactQuery logs a link without its query, which may carry anything.
func redactQuery(u *url.URL) string {
	c := *u
	c.RawQuery, c.Fragment = "", ""
	return c.String()
}

func (a *App) finishImport(job *soundImport, saved importer.Saved, err error) {
	if errors.Is(err, context.Canceled) {
		return // quitting
	}
	if err != nil {
		logx.Printf("import from %s failed: %v", job.host, err)
		job.state, job.err = importFailed, importMessage(err)
		a.render()
		return
	}
	logx.Printf("import: saved %s", saved.Path)
	for _, other := range a.imports.jobs {
		other.renaming = false // one name field at a time: the newest sound's
	}
	job.state, job.path, job.name, job.ext = importDone, saved.Path, saved.Name, filepath.Ext(saved.Path)
	job.renaming = true
	a.afterLibraryChange()
	if !a.tray.Popup().Visible() {
		job.renaming = false // nobody to type: keep the page's name
	}
	a.render()
}

// importMessage says what went wrong, in the panel's words.
func importMessage(err error) string {
	switch {
	case errors.Is(err, importer.ErrNoSound):
		return "No sound file found at this link. On Myinstants, drag a sound's name or its link icon; other links must lead to an MP3, WAV, FLAC or OGG file."
	case errors.Is(err, importer.ErrManySounds):
		return "This page has several sounds. Open the one you want and drag its link."
	case errors.Is(err, importer.ErrUnsupported):
		return "This isn't a sound Resound can play (MP3, WAV, FLAC or OGG)."
	case errors.Is(err, importer.ErrTooLarge):
		return fmt.Sprintf("The sound file is larger than %d MB.", importer.MaxSoundBytes>>20)
	case errors.Is(err, importer.ErrBlocked):
		return "Links to this PC or your local network are not downloaded."
	}
	return "The download failed: " + err.Error()
}

// renameImport saves the name typed for an imported sound.
func (a *App) renameImport(job *soundImport, typed string) {
	name := importer.SanitizeName(typed)
	if strings.TrimSpace(typed) == "" || name == job.name {
		job.renaming, job.err = false, ""
		a.render()
		return
	}
	dst := filepath.Join(job.dir, name+job.ext)
	if _, err := os.Lstat(dst); err == nil && !strings.EqualFold(dst, job.path) {
		job.err = "“" + name + "” is already in this folder. Choose another name."
		a.render()
		return
	}
	if err := os.Rename(job.path, dst); err != nil {
		logx.Printf("import: renaming %s: %v", job.path, err)
		job.err = "Could not rename the file: " + err.Error()
		a.render()
		return
	}
	logx.Printf("import: renamed to %s", dst)
	job.path, job.name, job.renaming, job.err = dst, name, false, ""
	a.afterLibraryChange()
	a.render()
}

// copySounds copies sound files dropped on a folder field into it (they
// are not moved: the originals stay where they were).
func (a *App) copySounds(dir string, files []string, skipped int) {
	a.libNote = "Copying " + plural(len(files), "sound") + "…"
	go func() {
		copied, failed := 0, 0
		for _, f := range files {
			if strings.EqualFold(filepath.Dir(f), filepath.Clean(dir)) {
				continue // already there
			}
			if _, err := importer.CopyUnique(f, dir); err != nil {
				logx.Printf("import: copying %s: %v", f, err)
				failed++
				continue
			}
			copied++
		}
		a.tray.Post(func() {
			parts := []string{fmt.Sprintf("Copied %s to “%s”.", plural(copied, "sound"), filepath.Base(dir))}
			if failed > 0 {
				parts = append(parts, plural(failed, "file")+" could not be copied (see log).")
			}
			if skipped > 0 {
				parts = append(parts, plural(skipped, "file")+" skipped: not MP3, WAV, FLAC or OGG.")
			}
			a.libNote = strings.Join(parts, " ")
			a.afterLibraryChange()
			a.render()
		})
	}()
}

// afterLibraryChange picks up files that were added or renamed.
func (a *App) afterLibraryChange() {
	a.imports.subdirs = nil
	a.rescan()
}

// importRows show the imports into folder dir under its field.
func (a *App) importRows(dir string) []ui.Row {
	var rows []ui.Row
	for _, job := range a.imports.jobs {
		if !strings.EqualFold(job.dir, dir) {
			continue
		}
		switch job.state {
		case importRunning:
			rows = append(rows, ui.Text("Downloading from "+job.host+"…", "", ui.ToneWarning))
		case importFailed:
			rows = append(rows, ui.Paragraph(job.err, ui.ToneBad))
		case importDone:
			if !job.renaming {
				rows = append(rows, ui.Text("Added “"+job.name+"”", "", ui.ToneGood))
				break
			}
			j := job
			fieldID := fmt.Sprintf("import-name-%d", j.id)
			rows = append(rows,
				ui.Text("Added to “"+filepath.Base(j.dir)+"”. Name it:", "", ui.ToneGood),
				ui.TextField(fieldID, j.name, importer.MaxNameLen,
					func(text string) { a.renameImport(j, text) },
					func() { j.renaming, j.err = false, ""; a.render() }),
			)
			if j.err != "" {
				rows = append(rows, ui.Paragraph(j.err, ui.ToneBad))
			}
			rows = append(rows, ui.Buttons(
				ui.Action{Label: "Keep name", OnClick: func() { j.renaming, j.err = false, ""; a.render() }},
				ui.Action{Label: "Save name", Style: ui.ButtonPrimary, OnClick: func() {
					if text, ok := a.tray.Popup().FieldText(fieldID); ok {
						a.renameImport(j, text)
					}
				}},
			))
		}
	}
	return rows
}

// subfolders lists a library folder's direct subfolders (each a tab once it
// holds sounds), cached until the library changes or the panel reopens.
func (a *App) subfolders(root string) []string {
	if a.imports.subdirs == nil {
		a.imports.subdirs = map[string][]string{}
	}
	if dirs, ok := a.imports.subdirs[root]; ok {
		return dirs
	}
	var dirs []string
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") {
				continue
			}
			dirs = append(dirs, filepath.Join(root, name))
			if len(dirs) == 50 {
				break
			}
		}
	}
	a.imports.subdirs[root] = dirs
	return dirs
}

// openMyinstants opens Myinstants in the browser. The panel stays open, so
// sounds can be dragged from the browser onto a folder.
func (a *App) openMyinstants() {
	if err := ui.ShellOpen(myinstantsURL); err != nil {
		logx.Printf("opening %s: %v", myinstantsURL, err)
	}
}
