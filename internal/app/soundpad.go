package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/soundpad"
	"github.com/motivated2die/resound/internal/ui"
)

// Carrying sounds and keys over from Soundpad, like the import from Soundux
// (soundux.go). Soundpad keeps sounds from anywhere in sound lists, grouped
// into categories at will; Resound's tabs are folders. So the user chooses
// lists, the page shows the folders their sounds are in, and the chosen
// folders are added. Folders that hold mostly other sounds start off.
// Hotbar sounds become favorites, colors the nearest of the phone's, and
// keys carry over where their encoding is known (soundpad/settings.go).
// Soundpad's devices and volume don't: the audio step sets those up.

// spPart is a part of the import besides the folders.
type spPart int

const (
	spFavorites spPart = iota // hotbar sounds
	spColors                  // the sounds' colors
	spKeys                    // push-to-talk, stop and pause keys
)

// soundpadState is Soundpad's lists and settings found on this PC.
type soundpadState struct {
	read     bool // reading started (once per run)
	running  bool // Soundpad runs: both would press and watch keys
	open     bool // the import page shows instead of the welcome page
	lists    []*soundpad.List
	chosen   map[string]bool // lists to carry over, by lower-case path
	settings soundpad.Settings
	plan     *soundpad.Plan  // of the chosen lists; nil while it is worked out
	planGen  int             // the plan being worked out
	folders  map[string]bool // folders the user turned on or off, by lower-case path
	off      map[spPart]bool
	listErr  string // why a chosen list couldn't be read
	err      string // why the import didn't run
}

// soundpadExe is Soundpad's program (input.AppName).
const soundpadExe = "soundpad.exe"

// readSoundpad reads Soundpad's settings (from the registry, for reading
// only) and its sound lists off the UI thread, chooses the current list and
// plans it. Once per run.
func (a *App) readSoundpad() {
	if a.soundpad.read || a.tray == nil {
		return
	}
	a.soundpad.read = true
	go func() {
		settings, recent := soundpad.ReadSettings()
		var lists []*soundpad.List
		for _, path := range soundpad.Discover(soundpad.Dir(), recent) {
			l, err := soundpad.Load(path)
			if err != nil {
				logx.Printf("soundpad: %s: %v", filepath.Base(path), err)
				continue
			}
			lists = append(lists, l)
		}
		if len(lists) == 0 && settings.Empty() {
			return
		}
		chosen := map[string]bool{}
		if len(lists) > 0 {
			chosen[strings.ToLower(lists[0].Path)] = true // the current list, if there is one
		}
		plan := soundpad.PlanLists(chooseLists(lists, chosen), soundpad.OS)
		running := input.RunningApps()[soundpadExe]
		logx.Printf("soundpad: found %d sound list(s), %d key(s); the current one has %d sound(s) in %d folder(s)",
			len(lists), len(settings.PushToTalk)+len(settings.Stop)+len(settings.Pause), len(plan.Sounds), len(plan.Folders))
		a.tray.Post(func() {
			st := &a.soundpad
			st.lists, st.chosen, st.settings, st.plan, st.running = lists, chosen, settings, &plan, running
			if a.page == pageWizard && a.wizardStep == 0 {
				a.render()
			}
		})
	}()
}

func chooseLists(lists []*soundpad.List, chosen map[string]bool) []*soundpad.List {
	var out []*soundpad.List
	for _, l := range lists {
		if chosen[strings.ToLower(l.Path)] {
			out = append(out, l)
		}
	}
	return out
}

// replanSoundpad works out the plan of the chosen lists again, off the UI
// thread (it checks every file).
func (a *App) replanSoundpad() {
	st := &a.soundpad
	st.planGen++
	gen, lists := st.planGen, chooseLists(st.lists, st.chosen)
	st.plan = nil
	go func() {
		plan := soundpad.PlanLists(lists, soundpad.OS)
		a.tray.Post(func() {
			if a.soundpad.planGen == gen {
				a.soundpad.plan = &plan
				a.render()
			}
		})
	}()
}

// soundpadFound reports whether there is something to offer.
func (a *App) soundpadFound() bool {
	st := &a.soundpad
	for _, l := range st.lists {
		if len(l.Sounds) > 0 {
			return true
		}
	}
	return !st.settings.Empty()
}

// soundpadOffer is the welcome page's offer to carry Soundpad's sounds and
// keys over, nil while there is nothing to offer.
func (a *App) soundpadOffer() []ui.Row {
	a.readSoundpad()
	if !a.soundpadFound() {
		return nil
	}
	st := &a.soundpad
	found := "keys"
	if len(st.lists) > 0 {
		found = plural(len(st.lists), "sound list")
		if !st.settings.Empty() {
			found += " and keys"
		}
	}
	return []ui.Row{
		ui.Spacer(6),
		ui.Offer(ui.IconImport, "Coming from Soundpad?",
			"Soundpad is set up on this PC with "+found+". Carry your sounds over with the folders they are in, then check them in the steps.",
			"Import from Soundpad…", a.openSoundpad),
	}
}

func (a *App) openSoundpad() {
	a.soundpad.open, a.soundpad.err, a.soundpad.listErr = true, "", ""
	go func() { // it may have started or quit since it was read
		running := input.RunningApps()[soundpadExe]
		a.tray.Post(func() {
			if a.soundpad.running != running {
				a.soundpad.running = running
				a.render()
			}
		})
	}()
	a.tray.Popup().ScrollToTop()
	a.render()
}

func (a *App) closeSoundpad() {
	a.soundpad.open, a.soundpad.err, a.soundpad.listErr = false, "", ""
	a.tray.Popup().ScrollToTop()
	a.render()
}

func (a *App) toggleSoundpadList(path string) {
	st := &a.soundpad
	if st.chosen == nil {
		st.chosen = map[string]bool{}
	}
	k := strings.ToLower(path)
	st.chosen[k] = !st.chosen[k]
	a.replanSoundpad()
	a.render()
}

// chooseSoundpadList adds a list saved anywhere, chosen in the file dialog.
func (a *App) chooseSoundpadList() {
	defer a.reopenPanel()
	path, ok, err := ui.OpenFileDialog(a.tray.Hwnd(), "Choose a Soundpad sound list",
		[]ui.FileFilter{{Name: "Soundpad sound lists (*.spl)", Pattern: "*.spl"}, {Name: "All files", Pattern: "*.*"}})
	if err != nil {
		logx.Printf("soundpad: open dialog: %v", err)
	}
	if !ok {
		return
	}
	l, err := soundpad.Load(path)
	if err != nil {
		a.soundpad.listErr = filepath.Base(path) + " couldn't be read as a Soundpad sound list."
		logx.Printf("soundpad: %s: %v", filepath.Base(path), err)
		return
	}
	st := &a.soundpad
	st.listErr = ""
	if st.chosen == nil {
		st.chosen = map[string]bool{}
	}
	k := strings.ToLower(l.Path)
	known := false
	for _, have := range st.lists {
		known = known || strings.ToLower(have.Path) == k
	}
	if !known {
		st.lists = append(st.lists, l)
	}
	st.chosen[k] = true
	a.replanSoundpad()
}

// spFolderOn reports whether a folder is to be added: the user's choice,
// else whether it holds mostly sounds of the lists.
func (a *App) spFolderOn(f soundpad.Folder) bool {
	if v, ok := a.soundpad.folders[strings.ToLower(f.Path)]; ok {
		return v
	}
	return f.Suggested()
}

func (a *App) toggleSpFolder(f soundpad.Folder) {
	if a.soundpad.folders == nil {
		a.soundpad.folders = map[string]bool{}
	}
	a.soundpad.folders[strings.ToLower(f.Path)] = !a.spFolderOn(f)
	a.render()
}

// spOn reports whether a part is chosen and has something to carry over.
func (a *App) spOn(part spPart) bool {
	st := &a.soundpad
	if st.off[part] {
		return false
	}
	switch part {
	case spFavorites:
		return st.plan != nil && st.plan.Favorites() > 0
	case spColors:
		return st.plan != nil && st.plan.Colored() > 0
	case spKeys:
		return !st.settings.Empty()
	}
	return false
}

func (a *App) toggleSp(part spPart) {
	if a.soundpad.off == nil {
		a.soundpad.off = map[spPart]bool{}
	}
	a.soundpad.off[part] = !a.soundpad.off[part]
	a.render()
}

// spReachable counts the plan's sounds that will be in the library: in a
// folder being added, or in the library already.
func (a *App) spReachable() int {
	p := a.soundpad.plan
	n := 0
	for _, s := range p.Sounds {
		in := a.folderCovering(s.Path)
		for _, f := range p.Folders {
			in = in || (a.spFolderOn(f) && within(s.Path, f.Path))
		}
		if in {
			n++
		}
	}
	return n
}

// soundpadPage chooses the lists, then the folders their sounds are in,
// then what else to carry over.
func (a *App) soundpadPage() []ui.Row {
	st := &a.soundpad
	rows := []ui.Row{
		ui.Header("Import from Soundpad", a.closeSoundpad),
		ui.Paragraph("Resound's tabs are folders: your sounds come with the folders they are in. Choose your sound lists, then the folders to add.", ui.ToneMuted),
	}
	if st.running {
		rows = append(rows, ui.Warning("Soundpad is running: quit it first",
			"Both apps would press your push-to-talk key and react to your keys. Quit Soundpad from its tray icon or its window.", nil))
	}

	rows = append(rows, ui.Section("Sound lists"))
	for _, l := range st.lists {
		path := l.Path
		rows = append(rows, ui.Toggle(a.spListName(l), st.chosen[strings.ToLower(path)], func() { a.toggleSoundpadList(path) }),
			ui.Paragraph(a.spListDetail(l), ui.ToneMuted))
	}
	rows = append(rows, ui.Buttons(ui.Action{Label: "Choose another list…", Icon: ui.IconFolderOpen, Closes: true, OnClick: a.chooseSoundpadList}))
	if st.listErr != "" {
		rows = append(rows, ui.Paragraph(st.listErr, ui.ToneBad))
	}

	p := st.plan
	chosen := false
	if p == nil {
		rows = append(rows, ui.Text("Reading your sound lists…", "", ui.ToneMuted))
	} else {
		rows = append(rows, ui.Section("Folders"))
		if len(p.Folders) == 0 {
			rows = append(rows, ui.Paragraph("No sounds Resound can play in the chosen lists.", ui.ToneMuted))
		}
		for _, f := range p.Folders {
			name := filepath.Base(f.Path)
			if a.folderCovering(f.Path) {
				rows = append(rows, ui.Text(name, "In your library", ui.ToneGood), ui.Paragraph(f.Path, ui.ToneMuted))
				continue
			}
			on := a.spFolderOn(f)
			chosen = chosen || on
			rows = append(rows, ui.Toggle(name, on, func() { a.toggleSpFolder(f) }), ui.Paragraph(spFolderDetail(f), ui.ToneMuted))
		}

		need := ""
		if a.spReachable() < len(p.Sounds) {
			need = " Sounds in folders you don't add stay out."
		}
		if p.Favorites() > 0 || p.Colored() > 0 || !st.settings.Empty() {
			rows = append(rows, ui.Section("Settings"))
		}
		if p.Favorites() > 0 {
			rows = append(rows, ui.Toggle("Hotbar sounds as favorites", a.spOn(spFavorites), func() { a.toggleSp(spFavorites) }),
				ui.Paragraph(plural(p.Favorites(), "sound")+" from your hotbar, also in the in-game overlay."+need, ui.ToneMuted))
		}
		if p.Colored() > 0 {
			rows = append(rows, ui.Toggle("Colors", a.spOn(spColors), func() { a.toggleSp(spColors) }),
				ui.Paragraph(plural(p.Colored(), "sound")+", as the nearest of the phone's colors."+need, ui.ToneMuted))
		}
	}
	if !st.settings.Empty() {
		rows = append(rows, ui.Toggle("Keys", a.spOn(spKeys), func() { a.toggleSp(spKeys) }), ui.Paragraph(a.spKeysDetail(), ui.ToneMuted))
	}
	for _, s := range st.settings.Skipped {
		rows = append(rows, ui.Paragraph(s, ui.ToneWarning))
	}
	if p != nil {
		rows = append(rows, ui.Spacer(4), ui.Paragraph(spNotCarried(p), ui.ToneMuted))
	}
	if st.err != "" {
		rows = append(rows, ui.Paragraph(st.err, ui.ToneBad))
	}

	chosen = chosen || a.spOn(spFavorites) || a.spOn(spColors) || a.spOn(spKeys)
	return append(rows, ui.Flex(), ui.Separator(),
		ui.Button("Import and continue", ui.ButtonPrimary, p == nil || !chosen, a.importSoundpad),
		ui.Button("Cancel", ui.ButtonNormal, false, a.closeSoundpad),
		ui.Spacer(4))
}

// spListName names a list on the page: Soundpad's current list as such,
// others by their file name.
func (a *App) spListName(l *soundpad.List) string {
	if l.Current {
		return "Current list"
	}
	return strings.TrimSuffix(filepath.Base(l.Path), filepath.Ext(l.Path))
}

func (a *App) spListDetail(l *soundpad.List) string {
	var d []string
	if l.Current {
		what := "What Soundpad shows now"
		if l.SavedAs != "" {
			what += ", saved as “" + strings.TrimSuffix(filepath.Base(l.SavedAs), filepath.Ext(l.SavedAs)) + "”"
		}
		d = append(d, what)
	} else if cur := a.spCurrent(); cur != nil && strings.EqualFold(cur.SavedAs, l.Path) {
		d = append(d, "The current list's file")
	}
	d = append(d, plural(len(l.Sounds), "sound"))
	if n := len(l.Categories); n > 0 {
		d = append(d, plural(n, "category"))
	}
	d = append(d, l.Modified.Local().Format("2 Jan, 15:04"))
	return strings.Join(d, " · ")
}

func (a *App) spCurrent() *soundpad.List {
	for _, l := range a.soundpad.lists {
		if l.Current {
			return l
		}
	}
	return nil
}

func spFolderDetail(f soundpad.Folder) string {
	of := fmt.Sprintf("%d", f.Files)
	if f.Many {
		of = "more than " + of
	}
	d := fmt.Sprintf("%s · %d of its %s sounds are in your lists", f.Path, f.Listed, of)
	if !f.Suggested() {
		return d + ", so it starts off."
	}
	return d + "."
}

func (a *App) spKeysDetail() string {
	s := &a.soundpad.settings
	var d []string
	for _, k := range s.PushToTalk {
		d = append(d, "Push-to-talk: "+input.Combo(k).String())
	}
	if s.Stop != nil {
		d = append(d, "Stop all sounds: "+input.Combo(s.Stop).String())
	}
	text := strings.Join(d, " · ")
	if s.Pause != nil {
		if text != "" {
			text += " · "
		}
		text += "Pause: " + input.Combo(s.Pause).String() + ". Soundpad's pause key switches; in Resound, sounds pause while you hold it"
	}
	return text + "."
}

// spNotCarried says what doesn't carry over.
func spNotCarried(p *soundpad.Plan) string {
	d := []string{"your audio devices and volume (the next steps set them up)"}
	if p.Categories > 0 {
		d = append(d, plural(p.Categories, "category")+" (Resound's tabs are your folders)")
	}
	if p.HotkeySounds > 0 {
		d = append(d, "keys of single sounds ("+plural(p.HotkeySounds, "sound")+"; the in-game overlay plays your favorites by key)")
	}
	if p.Unplayable > 0 {
		d = append(d, plural(p.Unplayable, "sound")+" in formats Resound doesn't play (it plays MP3, WAV, FLAC and OGG)")
	}
	if p.Missing > 0 {
		d = append(d, plural(p.Missing, "sound")+" whose files are gone")
	}
	return "Not carried over: " + strings.Join(d, "; ") + "."
}

// importSoundpad carries the chosen folders and parts over and continues
// with the setup's first step.
func (a *App) importSoundpad() {
	p := a.soundpad.plan
	if p == nil {
		return
	}
	if err := a.beginCarryOver("soundpad"); err != "" {
		a.soundpad.err = err
		a.render()
		return
	}
	var paths []string
	for _, f := range p.Folders {
		if a.spFolderOn(f) && !a.folderCovering(f.Path) {
			paths = append(paths, f.Path)
		}
	}
	note := a.carryOverFolders(paths)
	r := a.applySoundpad(p)
	if r.favorites+r.colors > 0 {
		note = append(note, fmt.Sprintf("From Soundpad: %s, %s.", plural(r.favorites, "favorite"), plural(r.colors, "color")))
	}
	logx.Printf("soundpad: imported: %d of %d folder(s) added, %d favorite(s), %d color(s), %d key(s)",
		len(paths), len(p.Folders), r.favorites, r.colors, r.keys)
	for _, n := range r.notes {
		logx.Printf("soundpad: %s", n)
	}
	a.soundpad = soundpadState{read: true} // carried over: not offered again
	a.finishCarryOver(append(note, r.notes...))
}

// spResult is what applySoundpad carried over.
type spResult struct {
	favorites, colors, keys int
	notes                   []string
}

// applySoundpad carries the chosen parts over, except the folders (added
// before). Like applySoundux it only fills what Resound doesn't have: a
// favorite is added, a color only where the sound has none, keys are added
// to the ones set. Nothing is saved or applied here.
func (a *App) applySoundpad(p *soundpad.Plan) spResult {
	var r spResult
	if a.spOn(spFavorites) || a.spOn(spColors) {
		for _, s := range p.Sounds {
			id, ok := a.lib.FindPath(s.Path)
			if !ok {
				continue // its folder wasn't added
			}
			fav, color := false, false
			_, _, err := a.lib.Update(id, func(st *library.SoundSettings) {
				if a.spOn(spFavorites) && s.Hotbar && !st.Favorite {
					st.Favorite, fav = true, true
				}
				if a.spOn(spColors) && s.Color != "" && st.Color == "" {
					st.Color, color = s.Color, true
				}
			})
			if err != nil {
				logx.Printf("soundpad: saving a sound's settings: %v", err)
				continue
			}
			if fav {
				r.favorites++
			}
			if color {
				r.colors++
			}
		}
	}
	if a.spOn(spKeys) {
		s := &a.soundpad.settings
		for _, k := range []struct {
			fn   keyFunc
			keys [][]uint16
		}{{fnPTT, s.PushToTalk}, {fnStop, nonNil(s.Stop)}, {fnPause, nonNil(s.Pause)}} {
			n, notes := a.carryOverKeys("Soundpad", k.fn, k.keys)
			r.keys += n
			r.notes = append(r.notes, notes...)
		}
	}
	return r
}

// nonNil is a single combination as a list, none for nil.
func nonNil(k []uint16) [][]uint16 {
	if k == nil {
		return nil
	}
	return [][]uint16{k}
}
