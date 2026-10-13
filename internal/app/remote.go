package app

import (
	"errors"
	"fmt"
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
	if size != snd.Size || !mod.Equal(snd.Modified) {
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

func (a *App) rescan() {
	start := time.Now()
	n := a.lib.Scan(a.cfg.Folders)
	tabs, _ := a.lib.Stats()
	logx.Printf("library: %d sound(s) in %d tab(s) from %d folder(s) in %v",
		n, tabs, len(a.cfg.Folders), time.Since(start).Round(time.Millisecond))
}

// --- indexer ------------------------------------------------------------

// startIndexer fingerprints new sounds and reads their lengths from the file
// headers in the background.
func (a *App) startIndexer() {
	ix := library.NewIndexer(a.lib, audio.ProbeDuration)
	ix.Logf = logx.Printf
	ix.OnProgress = func() { a.tray.Post(a.renderIndexProgress) }
	a.indexer = ix
	a.lib.OnChange = ix.Kick
	ix.Start()
	go removeOldCache()
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
// Explorer. A dropped sound file adds the folder it is in. Folders already in
// the library, or already one of its tabs (a direct subfolder), are skipped.
// libNote says what happened.
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
		a.cfg.Folders = append(a.cfg.Folders, added...)
		a.save()
		for _, p := range added {
			logx.Printf("library: added %s", p)
		}
		_, before := a.lib.Stats()
		a.rescan()
		_, after := a.lib.Stats()
		if len(added) == 1 {
			note = append(note, fmt.Sprintf("Added “%s”: %s.", filepath.Base(added[0]), plural(after-before, "sound")))
		} else {
			note = append(note, fmt.Sprintf("Added %d folders: %s.", len(added), plural(after-before, "sound")))
		}
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
	a.libNote = strings.Join(note, " ")
	a.render()
}

// folderCovering reports whether p is a library folder already, or one of
// its direct subfolders (which are tabs of it).
func (a *App) folderCovering(p string) bool {
	lp := strings.ToLower(filepath.Clean(p))
	for _, f := range a.cfg.Folders {
		lf := strings.ToLower(filepath.Clean(f))
		if lp == lf || strings.EqualFold(filepath.Dir(lp), lf) {
			return true
		}
	}
	return false
}

// acceptsDrops reports whether the current page adds folders by drag and
// drop (see render).
func (a *App) acceptsDrops() bool {
	return a.page == pageLibrary || (a.page == pageWizard && a.wizardStep == 1)
}

// refreshLibrary rescans the folders and says what changed.
func (a *App) refreshLibrary() {
	_, before := a.lib.Stats()
	a.rescan()
	_, after := a.lib.Stats()
	switch d := after - before; {
	case d > 0:
		a.libNote = fmt.Sprintf("Found %s.", plural(d, "new sound"))
	case d < 0:
		a.libNote = fmt.Sprintf("%s no longer found.", plural(-d, "sound"))
	default:
		a.libNote = "Up to date. Renamed and edited files are picked up too."
	}
	a.render()
}

// removeFolder removes a sound folder. Its sounds' settings are kept for 30
// days, so re-adding the folder restores them.
func (a *App) removeFolder(path string) {
	a.cfg.Folders = slices.DeleteFunc(slices.Clone(a.cfg.Folders), func(f string) bool { return f == path })
	a.save()
	a.removing = ""
	logx.Printf("library: removed %s", path)
	a.rescan()
	a.render()
}
