package app

// The sounds of a library folder on the library page: clicking a folder's
// field opens a slim list of its sounds below it (its subfolders have their
// own fields), each with tools to edit it in the editor (edit.go), rename
// it, or move it to the Recycle Bin.

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/motivated2die/resound/internal/importer"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// soundLists lives on App (UI thread).
type soundLists struct {
	open      map[string]bool // folders whose sounds are shown, by folderKey
	renaming  uint64          // the sound whose name field is shown, 0 for none
	renameErr string          // why the typed name wasn't taken
}

func folderKey(dir string) string { return strings.ToLower(filepath.Clean(dir)) }

func (s *soundLists) isOpen(dir string) bool { return s.open[folderKey(dir)] }

// toggleFolder opens or closes a folder's sound list.
func (a *App) toggleFolder(dir string) {
	s := &a.lists
	if s.open == nil {
		s.open = map[string]bool{}
	}
	k := folderKey(dir)
	if s.open[k] {
		delete(s.open, k)
	} else {
		s.open[k] = true
	}
	a.render()
}

// soundsByFolder groups the library's sounds by the folder they are in.
func (a *App) soundsByFolder() map[string][]library.Sound {
	out := map[string][]library.Sound{}
	for _, t := range a.lib.Tabs() {
		out[folderKey(t.Path)] = t.Sounds
	}
	return out
}

// soundRows list the sounds directly in folder dir, by name, indented to
// depth: a placeholder when there are none, the name field for the one
// being renamed, and for one open in the editor a note in place of its
// tools.
func (a *App) soundRows(sounds []library.Sound, depth int) []ui.Row {
	if len(sounds) == 0 {
		return []ui.Row{ui.SoundItem(ui.SoundSpec{Name: "No sounds in this folder", Depth: depth, Muted: true})}
	}
	sounds = slices.Clone(sounds)
	slices.SortFunc(sounds, func(x, y library.Sound) int {
		return cmp.Or(cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)), cmp.Compare(x.Name, y.Name))
	})
	var rows []ui.Row
	for _, s := range sounds {
		if a.lists.renaming == s.ID {
			rows = append(rows, a.renameRows(s)...)
			continue
		}
		spec := ui.SoundSpec{Name: s.Name, Tip: s.Path, Depth: depth}
		if a.editingSound(s.Path) {
			spec.Note = "Editing…"
		} else {
			path, name, id := s.Path, s.Name, s.ID
			spec.Tools = []ui.Tool{
				{Icon: ui.IconScissors, Tip: "Trim or edit in the editor", OnClick: func() { a.editSound(path) }},
				{Icon: ui.IconPencil, Tip: "Rename", OnClick: func() { a.startRename(id) }},
				{Icon: ui.IconTrash, Tip: "Move to the Recycle Bin", Danger: true, OnClick: func() { a.confirmDelete(path, name) }},
			}
		}
		rows = append(rows, ui.SoundItem(spec))
	}
	return rows
}

// editingSound reports whether a sound is open in the editor.
func (a *App) editingSound(path string) bool {
	return slices.ContainsFunc(a.imports.jobs, func(j *soundImport) bool {
		return j.state == importEditing && strings.EqualFold(j.replace, path)
	})
}

// --- rename ---------------------------------------------------------------------

func (a *App) startRename(id uint64) {
	a.lists.renaming, a.lists.renameErr = id, ""
	a.render()
}

func (a *App) cancelRename() {
	a.lists.renaming, a.lists.renameErr = 0, ""
	a.render()
}

func (a *App) renameRows(s library.Sound) []ui.Row {
	fieldID := fmt.Sprintf("rename-%d", s.ID)
	rows := []ui.Row{ui.TextField(fieldID, s.Name, importer.MaxNameLen,
		func(text string) { a.renameSound(s, text) },
		a.cancelRename)}
	if a.lists.renameErr != "" {
		rows = append(rows, ui.Paragraph(a.lists.renameErr, ui.ToneBad))
	}
	return append(rows, ui.Buttons(
		ui.Action{Label: "Cancel", OnClick: a.cancelRename},
		ui.Action{Label: "Rename", Style: ui.ButtonPrimary, OnClick: func() {
			if text, ok := a.tray.Popup().FieldText(fieldID); ok {
				a.renameSound(s, text)
			}
		}},
	))
}

// renameSound gives a sound's file the typed name (cleaned like a
// download's), in its folder and with its extension. The library matches
// the renamed file to the sound (file ID), so it keeps its settings.
func (a *App) renameSound(s library.Sound, typed string) {
	name := importer.SanitizeName(typed)
	if strings.TrimSpace(typed) == "" || name == s.Name {
		a.cancelRename()
		return
	}
	dst := filepath.Join(filepath.Dir(s.Path), name+filepath.Ext(s.Path))
	if _, err := os.Lstat(dst); err == nil && !strings.EqualFold(dst, s.Path) {
		a.lists.renameErr = "“" + name + "” is already in this folder. Choose another name."
		a.render()
		return
	}
	a.stopSound(s.Path) // Windows can't rename a file that is playing
	if err := renameRetry(s.Path, dst); err != nil {
		logx.Printf("library: renaming %s: %v", s.Path, err)
		a.lists.renameErr = "Could not rename the file: " + err.Error()
		a.render()
		return
	}
	logx.Printf("library: renamed %s to %s", s.Path, dst)
	a.lists.renaming, a.lists.renameErr = 0, ""
	a.rescan()
	a.libraryChanged()
	a.noteThenForget(a.imports.add(&soundImport{dir: filepath.Dir(s.Path), state: importDone, name: name, verb: "Renamed to"}))
}

// --- delete ---------------------------------------------------------------------

// confirmDelete asks before moving a sound to the Recycle Bin. The dialog
// runs on its own goroutine; the panel stays.
func (a *App) confirmDelete(path, name string) {
	go func() {
		ok := ui.Confirm(ui.DialogSpec{
			Title:   "Delete “" + name + "”?",
			Message: "“" + filepath.Base(path) + "” moves to the Recycle Bin. Its favorite, volume and phone color are kept for 30 days, in case you restore it.",
			Confirm: "Delete",
			Cancel:  "Cancel",
			Danger:  true,
		})
		if ok {
			a.tray.Post(func() { a.deleteSound(path, name) })
		}
	}()
}

// deleteSound moves a sound's file to the Recycle Bin. The library keeps
// its record for 30 days (a tombstone), so restoring the file restores
// its settings.
func (a *App) deleteSound(path, name string) {
	a.stopSound(path) // Windows can't move a file that is playing
	dir := filepath.Dir(path)
	go func() {
		err := retry(func() error { return ui.RecycleFile(path) })
		a.tray.Post(func() {
			if err != nil {
				logx.Printf("library: deleting %s: %v", path, err)
				a.failImport(a.imports.add(&soundImport{dir: dir}), "Could not delete “"+name+"”: "+err.Error())
				return
			}
			logx.Printf("library: moved %s to the Recycle Bin", path)
			a.rescan()
			a.libraryChanged()
			a.noteThenForget(a.imports.add(&soundImport{dir: dir, state: importDone, name: name, verb: "Deleted"}))
		})
	}()
}

// stopSound stops a sound wherever it plays (also an import's preview),
// so its file can be renamed, replaced or deleted. UI thread.
func (a *App) stopSound(path string) {
	id, ok := a.lib.FindPath(path)
	if !ok {
		return
	}
	a.playback.vmu.Lock()
	var voices []uint64
	for v, m := range a.playback.voices {
		if m.soundID == id {
			voices = append(voices, v)
		}
	}
	a.playback.vmu.Unlock()
	for _, v := range voices {
		a.engine.Stop(v)
	}
}
