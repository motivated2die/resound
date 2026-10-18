package app

// Cutting sounds from files Resound can't play: a video, or audio in
// another format, dropped onto a folder opens the app's edit page in a
// browser window, which embeds the editor (audiocutter.online) with the
// file loaded. Every clip the user adds there is saved into the folder and
// named on the usual card (import.go). The file goes from this PC to the
// browser and back over package editbridge; the app's host never sees it.

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/editbridge"
	"github.com/motivated2die/resound/internal/importer"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

const (
	// maxEdits is how many files one drop opens in the editor: each gets a
	// browser window.
	maxEdits = 3
	// maxClipBytes limits a clip coming back from the editor (FLAC: about
	// 10 minutes of 24-bit stereo at 48 kHz).
	maxClipBytes = 100 << 20
)

// editable reports whether a dropped file is one to cut in the editor:
// one the editor takes and Resound can't play itself.
func editable(path string) bool {
	return editbridge.Editable(path) && !playable(path)
}

func playable(path string) bool {
	return slices.Contains(audio.SupportedExtensions, strings.ToLower(filepath.Ext(path)))
}

// dropHint is what a folder field says while files are dragged over it:
// files that only the editor takes are cut there.
func dropHint(d ui.DropData) string {
	if slices.ContainsFunc(d.Paths, editable) && !slices.ContainsFunc(d.Paths, playable) {
		return "Drop to cut in the editor"
	}
	return ""
}

// editBridge returns the bridge to the edit page, set up on first use.
func (a *App) editBridge() *editbridge.Bridge {
	s := &a.imports
	if s.bridge == nil {
		s.bridge = editbridge.New(editbridge.Options{
			Origins:  []string{appOrigin(a.appURL())},
			OnOpen:   func(token string) { a.tray.Post(func() { a.editorOpened(token) }) },
			OnResult: a.saveClip,
			OnClose:  func(token string) { a.tray.Post(func() { a.editorClosed(token) }) },
		})
	}
	return s.bridge
}

// appOrigin is the origin the edit page runs at, e.g.
// "https://getresound.app".
func appOrigin(app string) string {
	u, err := url.Parse(app)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// editFiles opens files dropped onto folder dir in the editor.
func (a *App) editFiles(dir string, files []string) {
	if len(files) > maxEdits {
		a.setImportWarn(fmt.Sprintf("Only %d files open in the editor at once. Drop the others when you're done with these.", maxEdits))
		files = files[:maxEdits]
	}
	for _, f := range files {
		a.editFile(dir, f)
	}
}

func (a *App) editFile(dir, path string) {
	job := a.imports.add(&soundImport{dir: dir, host: "editor", state: importEditing, src: filepath.Base(path)})
	token, port, err := a.editBridge().Add(path, dir)
	if err != nil {
		logx.Printf("edit: %s: %v", path, err)
		msg := "The file could not be opened in the editor: " + err.Error()
		if errors.Is(err, editbridge.ErrTooLarge) {
			msg = fmt.Sprintf("“%s” is larger than %d GB, too large for the editor in the browser.", job.src, editbridge.MaxSourceBytes>>30)
		}
		a.failImport(job, msg)
		return
	}
	job.token = token
	job.editURL = editbridge.PageURL(a.appURL(), port, token)
	logx.Printf("edit: %s opened for %s", path, dir)
	a.showEditor(job)
}

// showEditor opens the edit page for job: in Edge's app mode, a window of
// its own, or in the default browser without Edge. A page already open for
// it keeps working too.
func (a *App) showEditor(job *soundImport) {
	if err := ui.ShellRun("msedge.exe", "--app="+job.editURL+" --window-size=1280,860"); err != nil {
		logx.Printf("edit: Edge: %v; using the default browser", err)
		if err := ui.ShellOpen(job.editURL); err != nil {
			logx.Printf("edit: %v", err)
			a.failImport(job, "The editor could not be opened: "+err.Error())
		}
	}
}

// stopEditing ends job: clips already added stay, the page can't add more.
func (a *App) stopEditing(job *soundImport) {
	if job.token != "" && a.imports.bridge != nil {
		a.imports.bridge.Remove(job.token)
	}
	a.imports.remove(job)
	a.render()
}

func (a *App) editingJob(token string) *soundImport {
	for _, j := range a.imports.jobs {
		if j.state == importEditing && j.token == token {
			return j
		}
	}
	return nil
}

func (a *App) editorOpened(token string) {
	if job := a.editingJob(token); job != nil && !job.opened {
		job.opened = true
		a.render()
	}
}

// editorClosed: the page was closed. Its clips stay.
func (a *App) editorClosed(token string) {
	if job := a.editingJob(token); job != nil {
		logx.Printf("edit: editor closed for %s", job.dir)
		a.imports.remove(job)
		a.render()
	}
}

// saveClip saves a clip from the editor into the job's folder under the
// edited file's name, then shows the naming card as for a download. It runs
// on the bridge's goroutine; its error is shown in the page.
func (a *App) saveClip(j editbridge.Job, body io.Reader) (string, error) {
	name := importer.SanitizeName(strings.TrimSuffix(filepath.Base(j.Path), filepath.Ext(j.Path)))
	if name == "" {
		name = "Clip"
	}
	saved, err := importer.Save(body, "."+editbridge.Format, name, j.Dir, maxClipBytes, audio.CheckFile)
	if err != nil {
		logx.Printf("edit: saving a clip into %s: %v", j.Dir, err)
		return "", errors.New(clipMessage(err))
	}
	dups, _ := a.lib.Duplicates(saved.Path) // before the rescan adds the new file
	a.tray.Post(func() {
		job := a.imports.add(&soundImport{dir: j.Dir, host: "editor", state: importRunning})
		a.finishImport(job, saved, dups, nil)
	})
	return saved.Name, nil
}

// clipMessage says why a clip from the editor wasn't saved.
func clipMessage(err error) string {
	switch {
	case errors.Is(err, importer.ErrTooLarge):
		return fmt.Sprintf("The clip is larger than %d MB. Cut a shorter part.", maxClipBytes>>20)
	case errors.Is(err, importer.ErrNoSound):
		return "The clip is empty."
	case errors.Is(err, importer.ErrUnsupported):
		return "Resound can't play this clip."
	}
	return "The clip could not be saved: " + err.Error()
}

// editRows show, under folder dir's field, that job's file is open in the
// editor, with buttons to bring the editor back and to stop.
func (a *App) editRows(job *soundImport) []ui.Row {
	text := "Opening “" + job.src + "” in the editor in your browser…"
	if job.opened {
		text = "“" + job.src + "” is open in the editor in your browser. Cut the part you want and press “Add to Resound”: each clip is saved here."
	}
	return []ui.Row{
		ui.Paragraph(text, ui.ToneMuted),
		ui.Buttons(
			ui.Action{Label: "Show editor", Icon: ui.IconExternalLink, OnClick: func() { a.showEditor(job) }},
			ui.Action{Label: "Stop editing", OnClick: func() { a.stopEditing(job) }},
		),
	}
}
