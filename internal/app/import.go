package app

// Importing sounds by drag and drop onto a folder on the Sound library page:
// a Myinstants sound (its name or link icon), any link to a sound file or a
// page with one sound, or sound files from Explorer. Videos and other audio
// files open in the editor in the browser (edit.go). Links are downloaded
// in the background (package importer). The new sound is saved under its
// page's name; a card under the folder lets the user rename it, preview it
// or discard it, and warns when the same sound is already in the library.
// The folder's own field shows the progress and, for a moment, the result.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/editbridge"
	"github.com/motivated2die/resound/internal/importer"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// The sites the library page links to; sounds dragged from them import.
const (
	myinstantsURL        = "https://www.myinstants.com/"
	soundButtonsWorldURL = "https://soundbuttonsworld.com/"
)

const (
	maxImportsShown = 6
	// importNoteFor is how long a folder shows "Added “…”" in place of
	// its sound count.
	importNoteFor = 3 * time.Second
	// importErrorFor is how long a failed import's message, or a warning
	// about a drop, stays on the page: long enough to read it.
	importErrorFor = 8 * time.Second
)

type importState int

const (
	importRunning   importState = iota // downloading
	importEditing                      // a file is open in the editor; its clips become imports of their own
	importNaming                       // saved; the card asks for a name
	importDone                         // kept: the folder shows a note
	importDiscarded                    // deleted again: the folder shows a note
	importFailed                       // nothing saved: a message under the folder
)

// soundImport is one dropped link, from download to the saved file.
type soundImport struct {
	id    int
	dir   string // the folder it goes into
	host  string // where it comes from
	state importState
	err   string // failed: what to tell the user; naming: a rename problem
	dup   string // naming: the same sound is already in the library

	src            string // editing: the file's name
	token, editURL string // editing: the file's token and the edit page's address
	opened         bool   // editing: the page has loaded the file

	path, name, ext string // the saved file
	voice           uint64 // its preview, while playing
}

// importsState lives on App (UI thread).
type importsState struct {
	jobs    []*soundImport
	seq     int
	fetcher *importer.Fetcher
	bridge  *editbridge.Bridge // to the edit page, once a file was opened in the editor
	ctx     context.Context
	cancel  context.CancelFunc
	warn    string // a drop that went nowhere, shown at the top of the page
	warnSeq int    // identifies the current warn, so an old timer leaves a newer one
}

func (s *importsState) init() {
	s.fetcher = importer.New(audio.SupportedExtensions, audio.CheckFile)
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

// clearFinished forgets everything but running downloads and files open
// in the editor (the panel reopened). A sound still waiting for its name
// keeps the name it has.
func (s *importsState) clearFinished() {
	s.jobs = slices.DeleteFunc(s.jobs, func(j *soundImport) bool { return j.state != importRunning && j.state != importEditing })
	s.warn = ""
}

// add numbers a new job and lists it. Beyond maxImportsShown, the oldest
// jobs are forgotten, except files open in the editor.
func (s *importsState) add(job *soundImport) *soundImport {
	s.seq++
	job.id = s.seq
	s.jobs = append(s.jobs, job)
	for excess := len(s.jobs) - maxImportsShown; excess > 0; excess-- {
		i := slices.IndexFunc(s.jobs, func(j *soundImport) bool { return j.state != importEditing })
		if i < 0 || i == len(s.jobs)-1 {
			break
		}
		s.jobs = slices.Delete(s.jobs, i, i+1)
	}
	return job
}

func (s *importsState) remove(job *soundImport) {
	s.jobs = slices.DeleteFunc(s.jobs, func(j *soundImport) bool { return j == job })
}

// onDropAnywhere handles drops on the library page (or the setup's sounds
// step) outside the folder fields: folders and sound files add library
// folders as before; links need a folder to go into.
func (a *App) onDropAnywhere(d ui.DropData) {
	if !a.acceptsDrops() {
		return
	}
	a.setImportWarn("")
	if len(d.Paths) > 0 {
		a.addFolders(d.Paths)
		if slices.ContainsFunc(d.Paths, editable) && len(a.cfg.Folders) > 0 {
			a.setImportWarn("Drop videos and other audio files onto one of the folders below to cut a sound from them.")
			a.render()
		}
		return
	}
	if len(d.Links) > 0 {
		if len(a.cfg.Folders) == 0 {
			a.setImportWarn("Add a sound folder first, then drop sounds onto it.")
		} else {
			a.setImportWarn("Drop the sound onto one of the folders below to download it there.")
		}
		a.render()
	}
}

// dropOnFolder returns the drop handler of the field for folder dir.
func (a *App) dropOnFolder(dir string) func(ui.DropData) {
	return func(d ui.DropData) {
		a.setImportWarn("")
		for _, link := range d.Links {
			a.importLink(dir, link)
		}
		var folders, sounds, edits []string
		skipped := 0
		for _, p := range d.Paths {
			info, err := os.Stat(p)
			switch {
			case err != nil:
				skipped++
			case info.IsDir():
				folders = append(folders, p)
			case playable(p):
				sounds = append(sounds, p)
			case editable(p):
				edits = append(edits, p)
			default:
				skipped++
			}
		}
		if len(folders) > 0 {
			a.addFolders(folders) // a folder dropped here becomes a library folder, as anywhere
		}
		if len(sounds) > 0 {
			a.copySounds(dir, sounds, skipped)
		} else if skipped > 0 && len(folders) == 0 && len(edits) == 0 {
			a.setImportWarn("Only sound files and videos can be added.")
		}
		if len(edits) > 0 {
			a.editFiles(dir, edits)
		}
		a.render()
	}
}

// importLink starts downloading a dropped link into dir.
func (a *App) importLink(dir, link string) {
	s := &a.imports
	u, ok := importer.ParseLink(link)
	if !ok {
		a.setImportWarn("That is not a web link to a sound.")
		return
	}
	job := s.add(&soundImport{dir: dir, host: u.Hostname()})
	logx.Printf("import: downloading %s into %s", redactQuery(u), dir)
	go func() {
		saved, err := s.fetcher.Fetch(s.ctx, u.String(), dir)
		var dups []library.Sound
		if err == nil {
			dups, _ = a.lib.Duplicates(saved.Path) // before the rescan adds the new file
		}
		a.tray.Post(func() { a.finishImport(job, saved, dups, err) })
	}()
}

// redactQuery logs a link without its query, which may carry anything.
func redactQuery(u *url.URL) string {
	c := *u
	c.RawQuery, c.Fragment = "", ""
	return c.String()
}

func (a *App) finishImport(job *soundImport, saved importer.Saved, dups []library.Sound, err error) {
	if errors.Is(err, context.Canceled) {
		return // quitting
	}
	if err != nil {
		logx.Printf("import from %s failed: %v", job.host, err)
		a.failImport(job, importMessage(err))
		return
	}
	logx.Printf("import: saved %s", saved.Path)
	job.path, job.name, job.ext = saved.Path, saved.Name, filepath.Ext(saved.Path)
	job.dup = a.duplicateWarning(dups)
	for _, other := range a.imports.jobs {
		if other != job && other.state == importNaming {
			a.finishNaming(other) // one card at a time: the newest sound's
		}
	}
	job.state = importNaming
	a.rescan()
	if !a.tray.Popup().Visible() {
		a.finishNaming(job) // nobody to ask: keep the page's name
		return
	}
	a.render()
}

// duplicateWarning says where the same sound already is, or "".
func (a *App) duplicateWarning(dups []library.Sound) string {
	if len(dups) == 0 {
		return ""
	}
	where := ""
	if _, tab, ok := a.lib.Sound(dups[0].ID); ok {
		where = " in " + tab
	}
	msg := "Already in your library: “" + dups[0].Name + "”" + where
	if len(dups) > 1 {
		msg += fmt.Sprintf(" and %s", plural(len(dups)-1, "more place"))
	}
	return msg + ". Keep it anyway, or discard this copy."
}

// importMessage says what went wrong, in the panel's words.
func importMessage(err error) string {
	switch {
	case errors.Is(err, importer.ErrNoSound):
		return "No sound file found at this link. On Myinstants or Sound Buttons World, drag one sound's name or its link; other links must lead to an MP3, WAV, FLAC or OGG file."
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

// saveImportName keeps the sound under the name typed for it (Enter or
// Save). An unchanged or empty name keeps the page's name.
func (a *App) saveImportName(job *soundImport, typed string) {
	name := importer.SanitizeName(typed)
	if strings.TrimSpace(typed) == "" || name == job.name {
		a.finishNaming(job)
		return
	}
	dst := filepath.Join(job.dir, name+job.ext)
	if _, err := os.Lstat(dst); err == nil && !strings.EqualFold(dst, job.path) {
		job.err = "“" + name + "” is already in this folder. Choose another name."
		a.render()
		return
	}
	a.stopImportPreview(job) // Windows can't rename a file that is open
	if err := renameRetry(job.path, dst); err != nil {
		logx.Printf("import: renaming %s: %v", job.path, err)
		job.err = "Could not rename the file: " + err.Error()
		a.render()
		return
	}
	logx.Printf("import: renamed to %s", dst)
	job.path, job.name = dst, name
	a.rescan()
	a.finishNaming(job)
}

// finishNaming ends the card: the sound stays, and its folder says so for
// a moment.
func (a *App) finishNaming(job *soundImport) {
	job.state, job.err = importDone, ""
	a.libraryChanged() // the import is complete: phones show the sound now
	a.noteThenForget(job)
}

// discardImport deletes the downloaded sound again.
func (a *App) discardImport(job *soundImport) {
	a.stopImportPreview(job)
	path := job.path
	job.state, job.err = importDiscarded, ""
	go func() {
		err := removeRetry(path)
		a.tray.Post(func() {
			if err != nil {
				logx.Printf("import: discarding %s: %v", path, err)
				a.failImport(job, "Could not delete the file: "+err.Error())
				return
			}
			logx.Printf("import: discarded %s", path)
			a.rescan()
			a.libraryChanged() // a phone may have seen the sound meanwhile
			a.noteThenForget(job)
		})
	}()
	a.render()
}

// failImport shows why an import failed under its folder, for
// importErrorFor.
func (a *App) failImport(job *soundImport, msg string) {
	job.state, job.err = importFailed, msg
	a.render()
	time.AfterFunc(importErrorFor, func() {
		a.tray.Post(func() {
			if job.state == importFailed {
				a.imports.remove(job)
				a.render()
			}
		})
	})
}

// setImportWarn shows a warning about a drop at the top of the library
// page for importErrorFor; "" clears it.
func (a *App) setImportWarn(text string) {
	s := &a.imports
	s.warnSeq++
	s.warn = text
	if text == "" {
		return
	}
	seq := s.warnSeq
	time.AfterFunc(importErrorFor, func() {
		a.tray.Post(func() {
			if s.warnSeq == seq && s.warn != "" {
				s.warn = ""
				a.render()
			}
		})
	})
}

// noteThenForget shows the job's note on its folder for importNoteFor.
func (a *App) noteThenForget(job *soundImport) {
	a.render()
	time.AfterFunc(importNoteFor, func() {
		a.tray.Post(func() {
			if job.state == importDone || job.state == importDiscarded {
				a.imports.remove(job)
				a.render()
			}
		})
	})
}

// previewImport plays the new sound on "You hear" only, from the start.
func (a *App) previewImport(job *soundImport) {
	a.stopImportPreview(job)
	path, name := job.path, job.name
	go func() { // decoding starts off the UI thread
		p, err := a.engine.PrepareSound(audio.SoundRef{Path: path, Gain: a.importGain(path)}, []string{audio.RoleMonitor})
		a.tray.Post(func() {
			if err == nil && job.state == importNaming && job.path == path {
				id, _ := a.lib.FindPath(path)
				var info audio.PlayInfo
				info, err = a.start(p, audio.PlayOptions{Roles: []string{audio.RoleMonitor}, Volume: map[string]float32{}},
					voiceMeta{soundID: id, name: name, preview: true}, false)
				if err == nil {
					job.voice = info.VoiceID
					a.render() // Preview becomes Stop
				}
			}
			if err != nil {
				logx.Printf("import: preview of %s: %v", path, err)
				job.err = "The preview could not play: " + err.Error()
				a.render()
			}
		})
	}()
}

// importGain measures a new sound for its preview, which usually comes
// before the indexer got to it, so the preview is as loud as the sound will
// play. Not on the UI thread.
func (a *App) importGain(path string) float32 {
	if !a.normalize.Load() {
		return 1
	}
	l, err := audio.MeasureLoudness(path)
	if err != nil {
		return 1 // the preview reports what's wrong with the file
	}
	return audio.NormGain(l.LUFS, l.Peak)
}

func (a *App) stopImportPreview(job *soundImport) {
	if job.voice != 0 {
		a.engine.Stop(job.voice)
		job.voice = 0
	}
}

// importPreviewEnded notes that a voice finished (stopped or played to its
// end): a card whose preview it was offers Preview again.
func (a *App) importPreviewEnded(voice uint64) {
	for _, job := range a.imports.jobs {
		if job.voice == voice && voice != 0 {
			job.voice = 0
			a.render()
		}
	}
}

// removeRetry and renameRetry wait for a stopped preview's decoder to
// close the file (it does so on its own goroutine, within milliseconds).
func removeRetry(path string) error {
	return retry(func() error {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
}

func renameRetry(from, to string) error { return retry(func() error { return os.Rename(from, to) }) }

func retry(fn func() error) error {
	var err error
	for range 20 {
		if err = fn(); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

// copySounds copies sound files dropped on a folder field into it (they
// are not moved: the originals stay where they were).
func (a *App) copySounds(dir string, files []string, skipped int) {
	a.setLibNote("Copying "+plural(len(files), "sound")+"…", false)
	go func() {
		copied, failed, known := 0, 0, 0
		for _, f := range files {
			if strings.EqualFold(filepath.Dir(f), filepath.Clean(dir)) {
				continue // already there
			}
			if dups, _ := a.lib.Duplicates(f); len(dups) > 0 {
				known++
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
			if known > 0 {
				verb := "were"
				if known == 1 {
					verb = "was"
				}
				parts = append(parts, fmt.Sprintf("%s already in your library; Windows-style names like “Name (2)” keep both.", plural(known, "sound")+" "+verb))
			}
			if failed > 0 {
				parts = append(parts, plural(failed, "file")+" could not be copied (see log).")
			}
			if skipped > 0 {
				parts = append(parts, plural(skipped, "file")+" skipped: not a sound or a video.")
			}
			a.setLibNote(strings.Join(parts, " "), true)
			a.rescan()
			a.libraryChanged()
			a.render()
		})
	}()
}

// folderImport is what a folder's field shows about imports into it: a
// note in place of its sound count, and whether one is in progress.
func (a *App) folderImport(dir string) (note string, active bool) {
	for _, job := range a.imports.jobs {
		if !strings.EqualFold(job.dir, dir) {
			continue
		}
		switch job.state {
		case importRunning:
			note, active = "Downloading…", true
		case importEditing:
			note, active = "Opening the editor…", true
			if job.opened {
				note = "Editing in your browser…"
			}
		case importNaming:
			note, active = "Name the new sound", true
		case importDone:
			note = "Added “" + job.name + "”"
		case importDiscarded:
			note = "Discarded “" + job.name + "”"
		}
	}
	return note, active
}

// importRows show, under folder dir's field, the card of a sound waiting
// for its name and the reasons imports failed.
func (a *App) importRows(dir string) []ui.Row {
	var rows []ui.Row
	for _, job := range a.imports.jobs {
		if !strings.EqualFold(job.dir, dir) {
			continue
		}
		switch job.state {
		case importFailed:
			rows = append(rows, ui.Paragraph(job.err, ui.ToneBad))
		case importEditing:
			rows = append(rows, a.editRows(job)...)
		case importNaming:
			j := job
			fieldID := fmt.Sprintf("import-name-%d", j.id)
			if j.dup != "" {
				rows = append(rows, ui.Paragraph(j.dup, ui.ToneWarning))
			}
			rows = append(rows, ui.TextField(fieldID, j.name, importer.MaxNameLen,
				func(text string) { a.saveImportName(j, text) },
				func() { a.finishNaming(j) }))
			if j.err != "" {
				rows = append(rows, ui.Paragraph(j.err, ui.ToneBad))
			}
			rows = append(rows, ui.Buttons(
				ui.Action{Label: "Discard", Icon: ui.IconTrash, Style: ui.ButtonDanger, OnClick: func() { a.discardImport(j) }},
				a.previewAction(j),
				ui.Action{Label: "Save", Style: ui.ButtonPrimary, OnClick: func() {
					if text, ok := a.tray.Popup().FieldText(fieldID); ok {
						a.saveImportName(j, text)
					}
				}},
			))
		}
	}
	return rows
}

// previewAction is the card's Preview button, or Stop while its preview
// plays.
func (a *App) previewAction(j *soundImport) ui.Action {
	if j.voice != 0 {
		return ui.Action{Label: "Stop", Icon: ui.IconStop, OnClick: func() { a.stopImportPreview(j); a.render() }}
	}
	return ui.Action{Label: "Preview", Icon: ui.IconPlay, OnClick: func() { a.previewImport(j) }}
}

// importGroup puts a folder's field and the rows of the import into it
// (its card, or why it failed) together. While an import into the folder
// runs, they share a lighter band, so the card visibly belongs to it.
func (a *App) importGroup(active bool, field ui.Row, rows []ui.Row) []ui.Row {
	out := []ui.Row{field}
	if active {
		out = []ui.Row{ui.GroupStart(), field}
	}
	out = append(out, rows...)
	if active {
		out = append(out, ui.GroupEnd())
	}
	return out
}

// openSite returns an action that opens a sound button site in the
// browser. The panel stays open, so sounds can be dragged from the browser
// onto a folder.
func (a *App) openSite(url string) func() {
	return func() {
		if err := ui.ShellOpen(url); err != nil {
			logx.Printf("opening %s: %v", url, err)
		}
	}
}
