package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/server"
	"github.com/motivated2die/resound/internal/ui"
)

// errUIBusy means the UI thread did not answer in time (e.g. shutting down).
var errUIBusy = errors.New("the app is busy, try again")

// onUI runs fn on the UI thread and waits for it. Server handlers use it so
// app state is only ever touched from one thread.
func (a *App) onUI(fn func()) bool {
	done := make(chan struct{})
	a.tray.Post(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

// remotePlayer adapts the App to server.Player.
type remotePlayer struct{ a *App }

var _ server.Player = remotePlayer{}

func (r remotePlayer) Play(soundID uint64, preview bool) (server.Started, error) {
	a := r.a
	snd, _, ok := a.lib.Sound(soundID)
	if !ok {
		return server.Started{}, server.ErrNotFound
	}
	// Sounds stream straight from their files. A file that changed since it
	// was indexed (new size or time) is re-indexed in the background.
	info0, err := os.Stat(snd.Path)
	if err != nil {
		a.tray.Post(a.rescan) // moved or deleted: the next list shows it
		return server.Started{}, fmt.Errorf("%s: %w: %w", snd.Name, err, server.ErrUnplayable)
	}
	size, mod := info0.Size(), info0.ModTime()
	length := snd.Duration
	changed := size != snd.Size || !mod.Equal(snd.Modified)
	if changed {
		a.lib.MarkChanged(soundID, size, mod)
		a.indexer.Kick()
		length = 0
	}
	ref := audio.SoundRef{
		Path:   snd.Path,
		Length: length,
		// Decoding to the end gives the exact length (probes of VBR files
		// without a VBR header are estimates).
		OnDecoded: func(d time.Duration) { a.lib.SetDuration(soundID, size, mod, d, true) },
	}
	if !changed { // an edited file's level is measured again first
		ref.Gain = a.normGain(snd.Level)
	}
	var roles []string
	if preview {
		roles = []string{audio.RoleMonitor}
	}
	// Buffer on this (HTTP) goroutine so the UI never waits for it.
	p, err := a.engine.PrepareSound(ref, roles)
	if errors.Is(err, audio.ErrUnreadable) {
		return server.Started{}, fmt.Errorf("%w: %w", err, server.ErrUnplayable)
	}
	if err != nil {
		return server.Started{}, err
	}
	opts := audio.PlayOptions{Volume: map[string]float32{}}
	if snd.LocalVolume != nil {
		opts.Volume[audio.RoleMonitor] = audio.Gain(*snd.LocalVolume)
	}
	if snd.RemoteVolume != nil {
		opts.Volume[audio.RoleCable] = audio.Gain(*snd.RemoteVolume)
	}
	if preview {
		opts.Roles = []string{audio.RoleMonitor}
	}
	var (
		info audio.PlayInfo
	)
	if !a.onUI(func() {
		info, err = a.start(p, opts, voiceMeta{soundID: soundID, name: snd.Name}, !preview)
	}) {
		return server.Started{}, errUIBusy
	}
	if err != nil {
		return server.Started{}, err
	}
	return server.Started{PlayingID: info.VoiceID, Length: info.Length}, nil
}

func (r remotePlayer) StopAll() { r.a.onUI(r.a.stopAll) }

// SetSoundVolume re-applies a sound's volumes to its playing voices. Engine
// and voice state are thread-safe, so it doesn't wait for the UI thread.
func (r remotePlayer) SetSoundVolume(soundID uint64, local, remote *int) {
	a := r.a
	vol := map[string]float32{}
	if local != nil {
		vol[audio.RoleMonitor] = audio.Gain(*local)
	}
	if remote != nil {
		vol[audio.RoleCable] = audio.Gain(*remote)
	}
	for _, v := range a.engine.Voices() {
		if meta, ok := a.playback.voice(v.ID); ok && meta.soundID == soundID {
			a.engine.SetVoiceVolume(v.ID, vol)
		}
	}
}

// Progress reads engine and voice state directly; it is polled several
// times per second and must not queue up on the UI thread.
func (r remotePlayer) Progress() []server.Progress {
	a := r.a
	var out []server.Progress
	for _, v := range a.engine.Voices() {
		meta, ok := a.playback.voice(v.ID)
		if !ok || meta.soundID == 0 {
			continue // test sounds are not shown on the phone
		}
		out = append(out, server.Progress{
			PlayingID: v.ID, SoundID: meta.soundID, Name: meta.name,
			Position: v.Position, Length: v.Length, Paused: v.Paused,
		})
	}
	return out
}

func (r remotePlayer) TogglePause() bool {
	var paused bool
	r.a.onUI(func() { paused = r.a.togglePause(); r.a.render() }) // the audio flow shows the pause
	return paused
}

func (r remotePlayer) SetTalkThrough(on bool) {
	r.a.onUI(func() { r.a.setTalkThrough(on); r.a.render() })
}

func (r remotePlayer) TogglePanel() bool {
	var open bool
	r.a.onUI(func() { open = r.a.tray.TogglePanel() })
	return open
}

func (r remotePlayer) DefaultVolumes() (local, remote int) {
	r.a.onUI(func() { local, remote = r.a.cfg.MonitorVolume, r.a.cfg.CableVolume })
	return
}

// --- server lifecycle (UI thread) ------------------------------------------

func (a *App) startServer() {
	auth, err := server.NewAuth(config.Dir())
	if err != nil {
		logx.Printf("remote auth: %v", err)
		a.serverErr = err
		return
	}
	auth.OnChange = func() { a.tray.Post(a.render) }
	a.srv = server.New(a.lib, remotePlayer{a}, auth, logx.Printf)
	a.srv.Name, _ = os.Hostname() // the app shows it in its list of PCs
	a.restartServer()
}

func (a *App) restartServer() {
	if a.srv == nil {
		return
	}
	a.serverErr = a.srv.Start(a.cfg.ServerPort) // logs a failure itself
	a.addresses = server.LocalAddresses()
	for _, ad := range a.addresses {
		logx.Printf("app: reachable at %s, UDP port %d (%s)", ad.IP, a.cfg.ServerPort, ad.Interface)
	}
}

func (a *App) resetRemote() {
	if a.srv == nil {
		return
	}
	if err := a.srv.Auth().ResetAll(); err != nil {
		logx.Printf("signing out all devices: %v", err)
	}
	logx.Printf("remote: all devices signed out")
	a.render()
}

// --- library (UI thread) ---------------------------------------------------

// libraryChanged tells the phones that are connected to reload the library,
// once at the end of something that changed it: a refresh, added or
// removed folders, a finished import (server.LibraryChanged).
func (a *App) libraryChanged() {
	if a.srv != nil {
		a.srv.LibraryChanged()
	}
}

func (a *App) rescan() {
	start := time.Now()
	n := a.lib.Scan(a.cfg.Folders)
	tabs, _ := a.lib.Stats()
	logx.Printf("library: %d sound(s) in %d tab(s) from %d folder(s) in %v",
		n, tabs, len(a.cfg.Folders), time.Since(start).Round(time.Millisecond))
}

// --- indexer ------------------------------------------------------------

// startIndexer fingerprints new sounds, reads their lengths from the file
// headers and measures their loudness in the background.
func (a *App) startIndexer() {
	ix := library.NewIndexer(a.lib, audio.ProbeDuration)
	ix.Measure, ix.LevelVersion = measureLevel, audio.LoudnessVersion
	ix.Logf = logx.Printf
	ix.OnProgress = func() { a.tray.Post(a.renderIndexProgress) }
	a.indexer = ix
	a.lib.OnChange = ix.Kick
	ix.Start()
	go removeOldCache()
}

// measureLevel adapts audio.MeasureLoudness to the indexer.
func measureLevel(path string) (library.Measurement, error) {
	l, err := audio.MeasureLoudness(path)
	return library.Measurement{LUFS: l.LUFS, Peak: l.Peak, Length: l.Length, Exact: l.Complete}, err
}

// removeOldCache deletes the decoded-audio cache of 0.6 development builds
// (sounds now always stream from their files). Runs on its own goroutine.
func removeOldCache() {
	dir := filepath.Join(config.Dir(), "cache")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var freed int64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".wav") && !strings.HasSuffix(name, ".tmp") {
			continue
		}
		var size int64
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		if os.Remove(filepath.Join(dir, name)) == nil {
			freed += size
		}
	}
	if err := os.Remove(dir); err == nil {
		logx.Printf("removed the old decoded-audio cache (%s)", audio.FormatBytes(freed))
	} else {
		logx.Printf("old decoded-audio cache partly removed (%s freed): %v", audio.FormatBytes(freed), err)
	}
}

// renderIndexProgress refreshes the panel for indexer progress, at most a
// few times per second and only on the pages that show it.
func (a *App) renderIndexProgress() {
	busy := a.indexer.Status().Busy
	shown := a.page == pageHome || a.page == pageLibrary || a.page == pageWizard || a.page == pageDiagnostics
	if !shown || (busy && time.Since(a.lastIndex) < 250*time.Millisecond) {
		return
	}
	a.lastIndex = time.Now()
	a.render()
}

// addFolder opens the folder picker, owned by the panel so it opens above
// it. If the panel was closed, it opens again afterwards.
func (a *App) addFolder() {
	pop := a.tray.Popup()
	owner := a.tray.Hwnd()
	if pop.Visible() {
		owner = pop.Hwnd()
	}
	path, ok := ui.BrowseFolder(owner, "Choose a sound folder. Its subfolders become extra tabs.")
	if ok {
		a.addFolders([]string{path})
	}
	if !pop.Visible() {
		a.reopenPanel()
	}
}

// addFolders adds sound folders chosen in the picker or dropped from
// Explorer, empty ones too (sounds can be put into them later). A dropped
// sound file adds the folder it is in. Folders already in the library, or
// inside one of its folders (scanned with it), are skipped; a folder that
// contains library folders replaces them. libNote says what happened.
func (a *App) addFolders(paths []string) {
	var added, known []string
	skipped := 0
	seen := map[string]bool{}
	for _, p := range paths {
		p = filepath.Clean(p)
		info, err := os.Stat(p)
		if err != nil {
			skipped++
			continue
		}
		if !info.IsDir() {
			if !slices.Contains(audio.SupportedExtensions, strings.ToLower(filepath.Ext(p))) {
				skipped++
				continue
			}
			p = filepath.Dir(p)
		}
		k := strings.ToLower(p)
		if seen[k] {
			continue
		}
		seen[k] = true
		if a.folderCovering(p) {
			known = append(known, p)
			continue
		}
		added = append(added, p)
	}

	var note []string
	if len(added) > 0 {
		// Library folders inside a new one are scanned with it from now on.
		var merged []string
		a.cfg.Folders = slices.DeleteFunc(slices.Clone(a.cfg.Folders), func(f string) bool {
			for _, p := range added {
				if within(f, p) {
					merged = append(merged, f)
					return true
				}
			}
			return false
		})
		for _, f := range merged {
			logx.Printf("library: %s is now part of a folder added above it", f)
		}
		if len(merged) > 0 {
			note = append(note, fmt.Sprintf("It already contained %s of the library; they are part of it now.", plural(len(merged), "folder")))
		}
		a.cfg.Folders = append(a.cfg.Folders, added...)
		a.save()
		for _, p := range added {
			logx.Printf("library: added %s", p)
		}
		_, before := a.lib.Stats()
		a.rescan()
		a.libraryChanged()
		_, after := a.lib.Stats()
		msg := fmt.Sprintf("Added %d folders: %s.", len(added), plural(after-before, "new sound"))
		if len(added) == 1 {
			msg = fmt.Sprintf("Added “%s”: %s.", filepath.Base(added[0]), plural(after-before, "new sound"))
		}
		note = append([]string{msg}, note...)
	}
	switch {
	case len(known) == 1:
		note = append(note, "“"+filepath.Base(known[0])+"” is already in the library.")
	case len(known) > 1:
		note = append(note, fmt.Sprintf("%d folders are already in the library.", len(known)))
	}
	if skipped > 0 {
		verb := "are"
		if skipped == 1 {
			verb = "is"
		}
		note = append(note, fmt.Sprintf("Skipped %s that %s neither a folder nor a sound file.", plural(skipped, "item"), verb))
	}
	a.setLibNote(strings.Join(note, " "), true)
	a.render()
}

// libNoteFor is how long a note about the library (a refresh, added
// folders, copied sounds) stays on the library and home pages.
const libNoteFor = 5 * time.Second

// setLibNote shows a note about the library. With expire, it disappears
// after libNoteFor unless a newer note replaced it; without (progress such
// as "Copying…"), it stays until the next note.
func (a *App) setLibNote(text string, expire bool) {
	a.libSeq++
	a.libNote = text
	if !expire || text == "" {
		return
	}
	seq := a.libSeq
	time.AfterFunc(libNoteFor, func() {
		a.tray.Post(func() {
			if a.libSeq == seq && a.libNote != "" {
				a.libNote = ""
				a.render()
			}
		})
	})
}

// folderCovering reports whether p is a library folder already, or inside
// one (its subfolders are scanned with it, at any depth).
func (a *App) folderCovering(p string) bool {
	for _, f := range a.cfg.Folders {
		if within(p, f) {
			return true
		}
	}
	return false
}

// within reports whether path is root or inside it (ignoring case).
func within(path, root string) bool {
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(root)), strings.ToLower(filepath.Clean(path)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// acceptsDrops reports whether the current page adds folders by drag and
// drop (see render).
func (a *App) acceptsDrops() bool {
	return a.page == pageLibrary || (a.page == pageWizard && a.wizardStep == 1)
}

// refreshLibrary drops library folders that no longer exist, rescans the
// others with all their subfolders (new and moved ones included), and says
// what changed.
func (a *App) refreshLibrary() {
	gone := a.pruneMissingFolders()
	_, before := a.lib.Stats()
	a.rescan()
	a.libraryChanged() // also when nothing changed: the phones pick up what they missed
	_, after := a.lib.Stats()
	var note []string
	switch d := after - before; {
	case d > 0:
		note = append(note, fmt.Sprintf("Found %s.", plural(d, "new sound")))
	case d < 0:
		note = append(note, fmt.Sprintf("%s no longer found.", plural(-d, "sound")))
	case len(gone) == 0:
		note = append(note, "Up to date. New folders, renamed and edited files are picked up too.")
	}
	switch {
	case len(gone) == 1:
		note = append(note, "“"+filepath.Base(gone[0])+"” no longer exists and was removed from the library.")
	case len(gone) > 1:
		note = append(note, fmt.Sprintf("%s no longer exist and were removed from the library.", plural(len(gone), "folder")))
	}
	if len(gone) > 0 {
		note = append(note, "Their sounds' settings are kept for 30 days in case they come back.")
	}
	a.setLibNote(strings.Join(note, " "), true)
	a.render()
}

// pruneMissingFolders removes library folders that are gone (deleted,
// moved or renamed in Explorer) and returns them. Only a folder Windows
// reports as missing is removed; one that can't be read for another reason
// (no access) stays.
func (a *App) pruneMissingFolders() []string {
	var gone []string
	kept := make([]string, 0, len(a.cfg.Folders))
	for _, f := range a.cfg.Folders {
		if _, err := os.Stat(f); errors.Is(err, fs.ErrNotExist) {
			gone = append(gone, f)
			continue
		}
		kept = append(kept, f)
	}
	if len(gone) > 0 {
		a.cfg.Folders = kept
		a.save()
		for _, f := range gone {
			logx.Printf("library: %s no longer exists; removed", f)
		}
	}
	return gone
}

// removeFolder removes a sound folder. Its sounds' settings are kept for 30
// days, so re-adding the folder restores them.
func (a *App) removeFolder(path string) {
	a.cfg.Folders = slices.DeleteFunc(slices.Clone(a.cfg.Folders), func(f string) bool { return f == path })
	a.save()
	a.removing = ""
	logx.Printf("library: removed %s", path)
	a.rescan()
	a.libraryChanged()
	a.render()
}
