package app

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/motivated2die/resound/internal/backup"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/soundux"
	"github.com/motivated2die/resound/internal/ui"
)

// Carrying settings over from Soundux. When its config is found, the setup's
// welcome page offers to import it; a page of switches chooses the parts,
// and the setup's steps then show what was carried over, for review. Its
// output devices never carry over: the audio step sets up the devices.
//
// Nothing in the file is trusted: package soundux reads it field by field
// and checks folders and files on the disk; here keys go through the same
// checks as recorded ones, folders through addFolders, settings only fill
// what Resound doesn't have yet, and a configured Resound is backed up first.

// sxPart is one part of the import, a switch on its page.
type sxPart int

const (
	sxFolders   sxPart = iota // sound folders, with their tabs' sorting
	sxFavorites               // favorite sounds
	sxVolumes                 // per-sound volumes
	sxKeys                    // push-to-talk and stop keys
	sxDefaults                // default volumes
	sxOptions                 // overlapping sounds, muting the mic
)

// sounduxState is Soundux's config found on this PC.
type sounduxState struct {
	plan    *soundux.Plan // nil: not read yet, not found, or nothing to carry over
	read    bool          // reading started (once per run)
	running bool          // Soundux runs: both would press and watch keys
	open    bool          // the import page shows instead of the welcome page
	off     map[sxPart]bool
	err     string // why the last import didn't run
}

// sounduxExe is Soundux's program (input.AppName).
const sounduxExe = "soundux.exe"

// readSoundux looks for Soundux's config off the UI thread (checking its
// folders and files can take a moment on network and cloud drives) and
// shows the offer once it is read. Once per run.
func (a *App) readSoundux() {
	if a.soundux.read || a.tray == nil {
		return
	}
	a.soundux.read = true
	go func() {
		path := soundux.Path()
		if path == "" {
			return
		}
		f, err := soundux.Load(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logx.Printf("soundux: %s: %v", filepath.Base(path), err)
			}
			return
		}
		plan := f.Plan(soundux.OS)
		running := input.RunningApps()[sounduxExe]
		logx.Printf("soundux: config found: %d folder(s) (%d missing), %d sound(s), %d favorite(s), %d sound volume(s), %d unreadable entr(ies)",
			len(plan.Folders), len(plan.MissingFolders), plan.SoundCount(), plan.Favorites(), plan.Volumes(), plan.Skipped)
		if plan.Empty() {
			return
		}
		a.tray.Post(func() {
			a.soundux.plan, a.soundux.running = &plan, running
			if a.page == pageWizard && a.wizardStep == 0 {
				a.render()
			}
		})
	}()
}

// sounduxOffer is the welcome page's offer to carry Soundux's settings
// over, nil while there is nothing to offer.
func (a *App) sounduxOffer() []ui.Row {
	a.readSoundux()
	p := a.soundux.plan
	if p == nil {
		return nil
	}
	var what []string
	if len(p.Folders) > 0 {
		what = append(what, plural(len(p.Folders), "sound folder"))
	}
	if p.PushToTalk != nil || p.Stop != nil {
		what = append(what, "keys")
	}
	what = append(what, "settings")
	found := strings.Join(what[:len(what)-1], ", ") + " and " + what[len(what)-1]
	if len(what) == 1 {
		found = what[0]
	}
	return []ui.Row{
		ui.Spacer(6),
		ui.Offer(ui.IconImport, "Coming from Soundux?",
			"Soundux is set up on this PC with "+found+". Carry them over, then check them in the steps.",
			"Import from Soundux…", a.openSoundux),
	}
}

func (a *App) openSoundux() {
	a.soundux.open, a.soundux.err = true, ""
	go func() { // it may have started or quit since it was read
		running := input.RunningApps()[sounduxExe]
		a.tray.Post(func() {
			if a.soundux.running != running {
				a.soundux.running = running
				a.render()
			}
		})
	}()
	a.tray.Popup().ScrollToTop()
	a.render()
}

func (a *App) closeSoundux() {
	a.soundux.open, a.soundux.err = false, ""
	a.tray.Popup().ScrollToTop()
	a.render()
}

// sxOn reports whether a part is chosen and has something to carry over.
func (a *App) sxOn(part sxPart) bool {
	p := a.soundux.plan
	if p == nil || a.soundux.off[part] {
		return false
	}
	switch part {
	case sxFolders:
		return len(p.Folders) > 0
	case sxFavorites:
		return p.Favorites() > 0
	case sxVolumes:
		return p.Volumes() > 0
	case sxKeys:
		return p.PushToTalk != nil || p.Stop != nil
	case sxDefaults:
		return p.MonitorVolume != nil || p.CableVolume != nil
	case sxOptions:
		return p.NoSoundOverlap != nil || p.MuteMic != nil
	}
	return false
}

func (a *App) toggleSx(part sxPart) {
	if a.soundux.off == nil {
		a.soundux.off = map[sxPart]bool{}
	}
	a.soundux.off[part] = !a.soundux.off[part]
	a.render()
}

// sounduxPage chooses what to carry over: a switch per part with what it
// holds, then what doesn't carry over.
func (a *App) sounduxPage() []ui.Row {
	p := a.soundux.plan
	rows := []ui.Row{
		ui.Header("Import from Soundux", a.closeSoundux),
		ui.Paragraph("Choose what to carry over. The setup then goes through its steps with it, so you can check everything.", ui.ToneMuted),
	}
	if a.soundux.running {
		rows = append(rows, ui.Warning("Soundux is running: quit it first",
			"Both apps would press your push-to-talk key and react to your keys. Quit Soundux from its tray icon.", nil))
	}
	part := func(pt sxPart, label, detail string) {
		if !a.sxOn(pt) && !a.soundux.off[pt] {
			return // nothing of this kind in Soundux
		}
		rows = append(rows, ui.Toggle(label, a.sxOn(pt), func() { a.toggleSx(pt) }), ui.Paragraph(detail, ui.ToneMuted))
	}

	part(sxFolders, "Sound folders", a.sxFoldersDetail())
	if len(p.MissingFolders) > 0 {
		rows = append(rows, ui.Paragraph(fmt.Sprintf("Not found, skipped: %s.", namesList(p.MissingFolders)), ui.ToneWarning))
	}
	reach := a.sxReachable()
	need := ""
	if reach < len(p.Sounds) {
		need = " Sounds outside your library need their folders."
	}
	part(sxFavorites, "Favorites", plural(p.Favorites(), "favorite sound")+"."+need)
	part(sxVolumes, "Sound volumes", "Volumes of their own for "+plural(p.Volumes(), "sound")+"."+need)
	part(sxKeys, "Push-to-talk and stop keys", a.sxKeysDetail())
	for _, d := range p.DroppedKeys {
		rows = append(rows, ui.Paragraph(d, ui.ToneWarning))
	}
	part(sxDefaults, "Default volumes", sxVolumesDetail(p))
	part(sxOptions, "Playback", sxOptionsDetail(p))

	notCarried := "Not carried over: your audio devices. The next steps set them up for Resound."
	if p.HotkeySounds > 0 {
		notCarried += fmt.Sprintf(" Keys of single sounds (%s): Resound plays sounds by key from the in-game overlay of your favorites.", plural(p.HotkeySounds, "sound"))
	}
	if p.MissingSounds > 0 {
		notCarried += fmt.Sprintf(" Settings of %s whose files are gone.", plural(p.MissingSounds, "sound"))
	}
	rows = append(rows, ui.Spacer(4), ui.Paragraph(notCarried, ui.ToneMuted))
	if a.soundux.err != "" {
		rows = append(rows, ui.Paragraph(a.soundux.err, ui.ToneBad))
	}

	chosen := false
	for pt := sxFolders; pt <= sxOptions; pt++ {
		chosen = chosen || a.sxOn(pt)
	}
	return append(rows, ui.Flex(), ui.Separator(),
		ui.Button("Import and continue", ui.ButtonPrimary, !chosen, a.importSoundux),
		ui.Button("Cancel", ui.ButtonNormal, false, a.closeSoundux),
		ui.Spacer(4))
}

func (a *App) sxFoldersDetail() string {
	p := a.soundux.plan
	d := plural(len(p.Folders), "folder") + " with " + plural(p.SoundCount(), "sound")
	covered := 0
	sorted := 0
	for _, f := range p.Folders {
		if f.Covered || a.folderCovering(f.Path) {
			covered++
		}
		if f.Sort != "" {
			sorted++
		}
	}
	switch {
	case covered == len(p.Folders):
		d += ", in your library already"
	case covered > 0:
		d += fmt.Sprintf(", %d in your library already", covered)
	}
	d += "."
	if sorted > 0 {
		d += fmt.Sprintf(" Tabs sorted by name stay sorted on your phone (%d).", sorted)
	}
	return d
}

// sxReachable counts the sounds with settings that will be in the library:
// inside its folders, or inside folders being carried over.
func (a *App) sxReachable() int {
	p := a.soundux.plan
	n := 0
	for _, s := range p.Sounds {
		in := a.folderCovering(s.Path)
		for _, f := range p.Folders {
			in = in || (a.sxOn(sxFolders) && within(s.Path, f.Path))
		}
		if in {
			n++
		}
	}
	return n
}

func (a *App) sxKeysDetail() string {
	p := a.soundux.plan
	var d []string
	if p.PushToTalk != nil {
		d = append(d, "Push-to-talk: "+input.Combo(p.PushToTalk).String())
	}
	if p.Stop != nil {
		d = append(d, "Stop all sounds: "+input.Combo(p.Stop).String())
	}
	return strings.Join(d, " · ") + "."
}

func sxVolumesDetail(p *soundux.Plan) string {
	var d []string
	if p.MonitorVolume != nil {
		d = append(d, fmt.Sprintf("You hear %d%%", *p.MonitorVolume))
	}
	if p.CableVolume != nil {
		d = append(d, fmt.Sprintf("Others hear %d%%", *p.CableVolume))
	}
	return strings.Join(d, " · ") + ", as loud as in Soundux."
}

func sxOptionsDetail(p *soundux.Plan) string {
	var d []string
	if p.NoSoundOverlap != nil {
		if *p.NoSoundOverlap {
			d = append(d, "One sound at a time")
		} else {
			d = append(d, "Sounds overlap")
		}
	}
	if p.MuteMic != nil {
		if *p.MuteMic {
			d = append(d, "Mic muted while any sound plays")
		} else {
			d = append(d, "Mic stays on while sounds play")
		}
	}
	return strings.Join(d, " · ") + "."
}

// namesList names up to three folders by their last part.
func namesList(paths []string) string {
	var names []string
	for i, p := range paths {
		if i == 3 {
			names = append(names, fmt.Sprintf("%d more", len(paths)-3))
			break
		}
		names = append(names, "“"+filepath.Base(p)+"”")
	}
	return strings.Join(names, ", ")
}

// importSoundux carries the chosen parts over and continues with the
// setup's first step, where the folders show.
func (a *App) importSoundux() {
	p := a.soundux.plan
	if p == nil {
		return
	}
	if err := a.beginCarryOver("soundux"); err != "" {
		a.soundux.err = err
		a.render()
		return
	}

	var note []string
	if a.sxOn(sxFolders) {
		var paths []string
		for _, f := range p.Folders {
			if !f.Covered {
				paths = append(paths, f.Path)
			}
		}
		note = append(note, a.carryOverFolders(paths)...)
	}
	r := a.applySoundux(p)
	if r.favorites+r.volumes > 0 {
		note = append(note, fmt.Sprintf("From Soundux: %s, %s.", plural(r.favorites, "favorite"), plural(r.volumes, "sound volume")))
	}
	logx.Printf("soundux: imported: folders %v, %d favorite(s), %d sound volume(s), %d sorted tab(s), %d key(s), %s",
		a.sxOn(sxFolders), r.favorites, r.volumes, r.sorted, r.keys, strings.Join(r.settings, ", "))
	for _, n := range r.notes {
		logx.Printf("soundux: %s", n)
	}

	a.soundux.plan, a.soundux.open = nil, false // carried over: not offered again
	a.finishCarryOver(append(note, r.notes...))
}

// beginCarryOver prepares carrying another soundboard's settings over (from
// is its name in the backup's file name and the log). A Resound set up
// before (the setup runs again, or folders already) is backed up first; it
// returns why the import can't run when that fails.
func (a *App) beginCarryOver(from string) string {
	if a.cfg.SetupComplete || len(a.cfg.Folders) > 0 {
		a.flushState()
		now := time.Now()
		e, err := backup.Create(config.Dir(), filepath.Join(backup.Dir(config.Dir()), backup.DefaultName(now, "before-"+from)), Version, now)
		if err != nil {
			logx.Printf("%s: backup before the import: %v", from, err)
			return "Nothing was carried over: the backup of your current settings failed (" + err.Error() + ")."
		}
		logx.Printf("%s: saved %s before the import", from, filepath.Base(e.Path))
	}
	if !a.cfg.SetupComplete {
		// Saved with the folders: a start with folders on the welcome page
		// would count as set up before the guided setup existed (Run).
		a.cfg.SetupStep = 1
	}
	return ""
}

// carryOverFolders adds an import's folders the way the panel adds folders
// (addFolders checks them again and skips ones in the library) and
// returns what it said.
func (a *App) carryOverFolders(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	a.addFolders(paths)
	if a.libNote == "" {
		return nil
	}
	return []string{a.libNote}
}

// finishCarryOver saves and applies everything an import changed, shows its
// note on the setup's first step and goes there.
func (a *App) finishCarryOver(note []string) {
	a.save()
	a.libraryChanged()
	a.applyVolumes()
	a.syncMic()
	a.keybindsChanged()
	a.setLibNote(strings.Join(slices.DeleteFunc(note, func(n string) bool { return n == "" }), " "), false)
	a.wizardGo(1)
}

// carryOverKeys adds another soundboard's combinations to fn's global keys,
// each through the checks a recorded key gets (keyConflict). It returns how
// many it added and why the others weren't; a combination Resound has
// there already is skipped silently.
func (a *App) carryOverKeys(from string, fn keyFunc, keys [][]uint16) (added int, notes []string) {
	for _, k := range keys {
		c := input.Combo(k)
		if msg := a.keyConflict(keyTarget{fn: fn}, c); msg != "" {
			if !strings.Contains(msg, "in this list already") {
				notes = append(notes, from+"'s "+strings.ToLower(fn.title())+" key "+c.String()+" wasn't added: "+msg)
			}
			continue
		}
		a.setGlobalKeys(fn, append(a.globalKeys(fn), slices.Clone(k)))
		added++
		if fn == fnPTT {
			a.cfg.PushToTalkWhilePlaying = true // the other soundboard held it while sounds played
		}
	}
	return added, notes
}

// sxResult is what applySoundux carried over.
type sxResult struct {
	favorites, volumes, sorted, keys int
	settings                         []string // names of the settings set, for the log
	notes                            []string // why something wasn't, for the panel
}

// applySoundux carries the chosen parts over, except the folders (added
// before, so their sounds are in the library). Settings only fill what
// Resound doesn't have: favorites are added, never removed; a sound's own
// volume and a tab's sorting are kept; keys are added to the ones set.
// Nothing is saved or applied here.
func (a *App) applySoundux(p *soundux.Plan) sxResult {
	var r sxResult
	if a.sxOn(sxFavorites) || a.sxOn(sxVolumes) {
		for _, s := range p.Sounds {
			id, ok := a.lib.FindPath(s.Path)
			if !ok {
				continue // not in the library (its folder wasn't carried over)
			}
			fav, vol := false, false
			_, _, err := a.lib.Update(id, func(st *library.SoundSettings) {
				if a.sxOn(sxFavorites) && s.Favorite && !st.Favorite {
					st.Favorite, fav = true, true
				}
				if a.sxOn(sxVolumes) && st.LocalVolume == nil && st.RemoteVolume == nil && (s.LocalVolume != nil || s.RemoteVolume != nil) {
					st.LocalVolume, st.RemoteVolume, vol = s.LocalVolume, s.RemoteVolume, true
				}
			})
			if err != nil {
				logx.Printf("soundux: saving a sound's settings: %v", err)
				continue
			}
			if fav {
				r.favorites++
			}
			if vol {
				r.volumes++
			}
		}
	}
	if a.sxOn(sxFolders) {
		for _, f := range p.Folders {
			if f.Sort != "" && a.sortTab(f.Path, f.Sort) {
				r.sorted++
			}
		}
	}
	if a.sxOn(sxKeys) {
		for _, k := range []struct {
			fn   keyFunc
			keys []uint16
		}{{fnPTT, p.PushToTalk}, {fnStop, p.Stop}} {
			if k.keys != nil {
				n, notes := a.carryOverKeys("Soundux", k.fn, [][]uint16{k.keys})
				r.keys += n
				r.notes = append(r.notes, notes...)
			}
		}
	}
	if a.sxOn(sxDefaults) {
		if p.MonitorVolume != nil {
			a.cfg.MonitorVolume = *p.MonitorVolume
			r.settings = append(r.settings, "you hear "+strconv.Itoa(*p.MonitorVolume)+"%")
		}
		if p.CableVolume != nil {
			a.cfg.CableVolume = *p.CableVolume
			r.settings = append(r.settings, "others hear "+strconv.Itoa(*p.CableVolume)+"%")
		}
	}
	if a.sxOn(sxOptions) {
		if p.NoSoundOverlap != nil {
			a.cfg.NoSoundOverlap = *p.NoSoundOverlap
			r.settings = append(r.settings, "one sound at a time "+strconv.FormatBool(*p.NoSoundOverlap))
		}
		if p.MuteMic != nil {
			// Soundux muted the mic for every sound: "Always".
			a.cfg.MuteMicWhilePlaying = *p.MuteMic
			if *p.MuteMic {
				a.cfg.MuteMicMinSeconds = 0
			}
			r.settings = append(r.settings, "mute mic "+strconv.FormatBool(*p.MuteMic))
		}
	}
	return r
}

// sortTab gives the tab of folder the phone's sorting, unless the user
// arranged or sorted it on the phone already. It reports whether it did.
func (a *App) sortTab(folder, sort string) bool {
	for _, t := range a.lib.Tabs() {
		if !strings.EqualFold(filepath.Clean(t.Path), filepath.Clean(folder)) {
			continue
		}
		set := false
		_, err := a.lib.UpdateLayout(strconv.FormatUint(t.ID, 10), func(l *library.Layout) {
			if l.Sort == "" && len(l.Orders) == 0 {
				l.Sort, set = sort, true
			}
		})
		if err != nil {
			logx.Printf("soundux: saving a tab's sorting: %v", err)
		}
		return set && err == nil
	}
	return false
}
