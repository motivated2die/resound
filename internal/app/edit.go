package app

// Cutting sounds from files Resound can't play: a video, or audio in
// another format, dropped onto a folder opens the app's edit page in a
// browser window, which embeds the editor (audiocutter.online) with the
// file loaded. Every clip the user adds there is saved into the folder and
// named on the usual card (import.go). A sound of the library opens there
// too (its scissors on the library page, sounds.go): what the editor saves
// replaces it. The file goes from this PC to the browser and back over
// package editbridge; the app's host never sees it.

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	// maxDockedEdits is how many files open in the editor the dock lists
	// one by one; beyond, a line says how many more (Stop all ends them).
	maxDockedEdits = 5
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
			// A page that finished (Close, a saved sound, Stop editing here)
			// and couldn't close its own window: Resound opened it and
			// closes it.
			CloseWindow: func(title string) {
				go func() { // it waits for the browser to show the title
					if !ui.CloseBrowserWindow(title, 2*time.Second) {
						logx.Printf("edit: the editor's window isn't an app window; the page asks to close it")
					}
				}()
			},
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
		a.openInEditor(dir, f, false)
	}
}

// editSound opens a sound of the library in the editor; what it saves
// replaces the sound. A sound open already brings its editor back.
func (a *App) editSound(path string) {
	if j := a.soundEditor(path); j != nil {
		a.showEditor(j)
		return
	}
	a.openInEditor(filepath.Dir(path), path, true)
}

// openInEditor opens path in the editor; new clips go into dir, or, with
// replace, the editor's saves replace the file.
func (a *App) openInEditor(dir, path string, replace bool) {
	job := a.imports.add(&soundImport{dir: dir, host: "editor", state: importEditing, src: filepath.Base(path), source: path})
	if replace {
		job.replace = path
	}
	token, port, err := a.editBridge().Add(path, dir, replace)
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

// stopEditing ends job: clips already added stay, the page can't add more
// and finishes, which closes its window.
func (a *App) stopEditing(job *soundImport) {
	a.endEditing(job)
	a.render()
}

// stopAllEditing ends every file open in the editor, as stopEditing.
func (a *App) stopAllEditing() {
	for _, j := range a.editingJobs() {
		a.endEditing(j)
	}
	a.render()
}

func (a *App) endEditing(job *soundImport) {
	if job.token != "" && a.imports.bridge != nil {
		a.imports.bridge.Stop(job.token)
	}
	a.imports.remove(job)
}

// editingJobs are the files open in the editor, in the order they opened.
func (a *App) editingJobs() []*soundImport {
	var out []*soundImport
	for _, j := range a.imports.jobs {
		if j.state == importEditing {
			out = append(out, j)
		}
	}
	return out
}

// soundEditor returns the job of a library sound open in the editor, or
// nil.
func (a *App) soundEditor(path string) *soundImport {
	for _, j := range a.editingJobs() {
		if strings.EqualFold(j.replace, path) {
			return j
		}
	}
	return nil
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
// edited file's name, then shows the naming card as for a download; for a
// sound of the library, it replaces the sound. It runs on the bridge's
// goroutine; its error is shown in the page.
func (a *App) saveClip(j editbridge.Job, body io.Reader) (string, error) {
	if j.Replace {
		return a.replaceSound(j, body)
	}
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

// replaceSound writes what the editor saved over the sound it edits: into
// a temporary file next to it first, checked, then renamed over it. The
// sound keeps its place in the library (same path: its ID and settings;
// its length and loudness are measured again). Bridge goroutine.
func (a *App) replaceSound(j editbridge.Job, body io.Reader) (string, error) {
	ext := filepath.Ext(j.Path)
	name := strings.TrimSuffix(filepath.Base(j.Path), ext)
	tmp, err := writeChecked(body, j.Dir, ext)
	if err != nil {
		logx.Printf("edit: replacing %s: %v", j.Path, err)
		return "", errors.New(clipMessage(err))
	}
	// Windows can't replace a file that is playing.
	a.onUI(func() { a.stopSound(j.Path) })
	if err := renameRetry(tmp, j.Path); err != nil {
		os.Remove(tmp)
		logx.Printf("edit: replacing %s: %v", j.Path, err)
		return "", errors.New("The sound could not be replaced: " + err.Error())
	}
	logx.Printf("edit: replaced %s", j.Path)
	a.tray.Post(func() {
		a.rescan()
		a.libraryChanged()
		a.noteThenForget(a.imports.add(&soundImport{dir: j.Dir, state: importDone, name: name, verb: "Updated"}))
	})
	return name, nil
}

// writeChecked writes a sound to a temporary file in dir (a name the
// library ignores, with extension ext, which the check decodes by) and
// checks that it plays. It returns the file's path.
func writeChecked(body io.Reader, dir, ext string) (string, error) {
	f, err := os.CreateTemp(dir, "~resound-*"+ext)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(body, maxClipBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err == nil && n > maxClipBytes:
		err = importer.ErrTooLarge
	case err == nil && n == 0:
		err = importer.ErrNoSound
	case err == nil:
		if cerr := audio.CheckFile(f.Name()); cerr != nil {
			err = fmt.Errorf("%w: %w", importer.ErrUnsupported, cerr)
		}
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
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

// editDock is the library page's dock while files are open in the editor:
// each file with buttons to bring its editor back and to stop editing it,
// and below them one button that stops them all. It stays in view while
// the page scrolls; the folders the files go into are highlighted too
// (folderImport). Nil while no file is open.
func (a *App) editDock() []ui.Row {
	jobs := a.editingJobs()
	if len(jobs) == 0 {
		return nil
	}
	shown := jobs
	if len(shown) > maxDockedEdits {
		shown = shown[:maxDockedEdits-1] // the last line says how many more
	}
	rows := []ui.Row{ui.Section("Open in the editor")}
	for _, j := range shown {
		detail := "Opening the editor…"
		switch {
		case j.opened && j.replace != "":
			detail = "Trimming the sound in “" + filepath.Base(j.dir) + "”"
		case j.opened:
			detail = "Cutting clips into “" + filepath.Base(j.dir) + "”"
		}
		rows = append(rows, ui.TaskItem(ui.TaskSpec{
			Icon: ui.IconScissors, Name: j.src, Detail: detail, Tip: j.source, Busy: j.opened,
			Tools: []ui.Tool{
				{Icon: ui.IconExternalLink, Tip: "Show the editor", OnClick: func() { a.showEditor(j) }},
				{Icon: ui.IconClose, Tip: "Stop editing: the clips you added stay", OnClick: func() { a.stopEditing(j) }},
			},
		}))
	}
	if more := len(jobs) - len(shown); more > 0 {
		rows = append(rows, ui.Paragraph(fmt.Sprintf("and %s more in the editor", plural(more, "file")), ui.ToneMuted))
	}
	label := "Stop editing"
	if len(jobs) > 1 {
		label = fmt.Sprintf("Stop editing all %d", len(jobs))
	}
	return append(rows, ui.Button(label, ui.ButtonNormal, false, a.stopAllEditing))
}
