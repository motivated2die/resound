package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/soundpad"
	"github.com/motivated2die/resound/internal/ui"
)

// Carrying sounds and keys over from Soundpad, like the import from Soundux
// (soundux.go). Soundpad keeps sounds from anywhere in sound lists, grouped
// into categories at will; Resound's tabs are folders. The user chooses one
// list (two would clash) and one of two ways:
//
//   - By category (the default): the sounds are copied into a folder per
//     category in a folder the user chooses, which is added. A sound in
//     several categories is copied into each; each tab gets Soundpad's
//     order.
//   - By folder: the folders the list's sounds are in are added as they
//     are, all of them at first. The categories and their order are lost.
//
// Either way, hotbar sounds become favorites, colors the nearest of the
// phone's, and keys carry over (soundpad/settings.go). Soundpad's devices
// and volume don't: the audio step sets those up.

// spPart is a part of the import besides the sounds.
type spPart int

const (
	spFavorites spPart = iota // hotbar sounds
	spColors                  // the sounds' colors
	spKeys                    // push-to-talk, stop and pause keys
	spMuteMic                 // muting the mic while sounds play
)

// spMode is how the sounds come over.
type spMode int

const (
	spByCategory spMode = iota // copied into a folder per category (the default)
	spByFolder                 // their folders are added
)

// soundpadState is Soundpad's lists and settings found on this PC.
type soundpadState struct {
	read     bool // reading started (once per run)
	running  bool // Soundpad runs: both would press and watch keys
	open     bool // the import page shows instead of the welcome page
	lists    []*soundpad.List
	list     string // the chosen list's path
	mode     spMode
	target   string // by category: the folder the category folders go in
	settings soundpad.Settings
	plan     *soundpad.Plan  // of the chosen list; nil while it is worked out
	planGen  int             // the plan being worked out
	folders  map[string]bool // by folder: folders the user switched, by lower-case path
	off      map[spPart]bool
	copying  *atomic.Bool // by category: set while copying; Store(true) asks it to stop
	copied   int          // files done so far
	listErr  string       // why a chosen list couldn't be read
	err      string       // why the import didn't run
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
		var plan soundpad.Plan
		chosen := ""
		if len(lists) > 0 {
			chosen = lists[0].Path // the current list, if there is one
			plan = soundpad.PlanList(lists[0], soundpad.OS)
		}
		running := input.RunningApps()[soundpadExe]
		logx.Printf("soundpad: found %d sound list(s), %d key(s); the first has %d sound(s) in %d folder(s) and %d categories",
			len(lists), len(settings.PushToTalk)+min(len(settings.Stop), 1)+min(len(settings.Pause), 1), len(plan.Sounds), len(plan.Folders), len(plan.Categories))
		a.tray.Post(func() {
			st := &a.soundpad
			st.lists, st.list, st.settings, st.plan, st.running = lists, chosen, settings, &plan, running
			if a.page == pageWizard && a.wizardStep == 0 {
				a.render()
			}
		})
	}()
}

// spList is the chosen list, nil for none.
func (a *App) spList() *soundpad.List {
	for _, l := range a.soundpad.lists {
		if strings.EqualFold(l.Path, a.soundpad.list) {
			return l
		}
	}
	return nil
}

// replanSoundpad works out the plan of the chosen list again, off the UI
// thread (it checks every file).
func (a *App) replanSoundpad() {
	st := &a.soundpad
	st.planGen++
	gen, l := st.planGen, a.spList()
	st.plan, st.folders = nil, nil
	go func() {
		var plan soundpad.Plan
		if l != nil {
			plan = soundpad.PlanList(l, soundpad.OS)
		}
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
	return !st.settings.Empty() || st.settings.MuteMic
}

// soundpadOffer is the welcome page's offer to carry Soundpad's sounds and
// keys over, nil while there is nothing to offer.
func (a *App) soundpadOffer() ui.Row {
	a.readSoundpad()
	if !a.soundpadFound() {
		return nil
	}
	st := &a.soundpad
	found := "settings"
	if len(st.lists) > 0 {
		found = plural(len(st.lists), "sound list")
		if !st.settings.Empty() || st.settings.MuteMic {
			found += " and settings"
		}
	}
	return ui.Offer(ui.IconImport, "Coming from Soundpad?",
		"Found "+found+". Carry your sounds over by category or by folder.",
		"Import from Soundpad…", a.openSoundpad)
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
	if a.soundpad.copying != nil {
		return
	}
	a.soundpad.open, a.soundpad.err, a.soundpad.listErr = false, "", ""
	a.tray.Popup().ScrollToTop()
	a.render()
}

func (a *App) chooseSoundpadList(path string) {
	st := &a.soundpad
	if strings.EqualFold(st.list, path) || st.copying != nil {
		return
	}
	st.list, st.err = path, ""
	a.replanSoundpad()
	a.render()
}

// openSoundpadList adds a list saved anywhere, chosen in the file dialog,
// and chooses it.
func (a *App) openSoundpadList() {
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
	if !slices.ContainsFunc(st.lists, func(have *soundpad.List) bool { return strings.EqualFold(have.Path, l.Path) }) {
		st.lists = append(st.lists, l)
	}
	a.chooseSoundpadList(l.Path)
}

func (a *App) setSoundpadMode(m int) {
	if a.soundpad.copying == nil {
		a.soundpad.mode, a.soundpad.err = spMode(m), ""
		a.render()
	}
}

// chooseSoundpadTarget asks where to copy the sounds to by category.
func (a *App) chooseSoundpadTarget() {
	defer a.reopenPanel()
	if path, ok := ui.BrowseFolder(a.tray.Hwnd(), "Choose the folder to copy your sounds into. A folder per category goes in it."); ok {
		a.soundpad.target, a.soundpad.err = filepath.Clean(path), ""
	}
}

// spFolderOn reports whether a folder is to be added: the user's choice,
// else on.
func (a *App) spFolderOn(f soundpad.Folder) bool {
	if v, ok := a.soundpad.folders[strings.ToLower(f.Path)]; ok {
		return v
	}
	return true
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
	case spMuteMic:
		return st.settings.MuteMic
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

// spReachable counts the plan's sounds that will be in the library by
// folder: in a folder being added, or in the library already.
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

// spTarget checks the folder to copy into: what to know about the copy
// (info), and why the folder can't be used (bad).
func (a *App) spTarget() (info, bad string) {
	st := &a.soundpad
	files, bytes := st.plan.Copies()
	info = fmt.Sprintf("Copies %s (%s) into a folder per category; a sound in several categories is copied into each. The originals stay where they are.",
		plural(files, "sound file"), spSize(bytes))
	t := st.target
	if t == "" {
		return info, ""
	}
	if filepath.Dir(t) == t {
		return info, "Choose a folder, not a whole drive: Resound adds it to your library."
	}
	if free, ok := soundpad.FreeSpace(t); ok {
		if uint64(bytes) > free {
			return info, fmt.Sprintf("Not enough room on %s: the copies need %s, %s is free.", filepath.VolumeName(t), spSize(bytes), spSize(int64(free)))
		}
		info += fmt.Sprintf(" %s is free on %s.", spSize(int64(free)), filepath.VolumeName(t))
	}
	if entries, err := os.ReadDir(t); err == nil && len(entries) > 0 {
		info += " The folder has files already: sounds in it come into your library too."
	}
	return info, ""
}

// spSize is a file size for the page: "840 KB", "22 MB", "1.4 GB".
func spSize(b int64) string {
	switch {
	case b >= 1<<30:
		return strconv.FormatFloat(float64(b)/(1<<30), 'f', 1, 64) + " GB"
	case b >= 1<<20:
		return strconv.FormatInt((b+1<<19)>>20, 10) + " MB"
	}
	return strconv.FormatInt((b+1<<9)>>10, 10) + " KB"
}

// soundpadPage chooses the list, how its sounds come over (and with that
// the folders to add, or where to copy them), then what else.
func (a *App) soundpadPage() []ui.Row {
	st := &a.soundpad
	copying := st.copying != nil
	back := a.closeSoundpad
	if copying {
		back = nil
	}
	rows := []ui.Row{
		ui.Header("Import from Soundpad", back),
		ui.Paragraph("Choose the sound list to carry over and how its sounds come into Resound, whose tabs are folders.", ui.ToneMuted),
	}
	if st.running {
		rows = append(rows, ui.Warning("Soundpad is running: quit it first",
			"Both apps would press your push-to-talk key and react to your keys. Quit Soundpad from its tray icon or its window.", nil))
	}

	rows = append(rows, ui.Section("Sound list"))
	options := make([]ui.Option, len(st.lists))
	chosen := -1
	for i, l := range st.lists {
		options[i] = ui.Option{Label: a.spListName(l)}
		if l.Current {
			options[i].Detail = "current"
		}
		if strings.EqualFold(l.Path, st.list) {
			chosen = i
		}
	}
	rows = append(rows, ui.DropdownTool("soundpad-list", ui.IconFolderOpen, "Choose another list…", a.openSoundpadList, options, chosen,
		func(i int) { a.chooseSoundpadList(st.lists[i].Path) }))
	if l := a.spList(); l != nil {
		rows = append(rows, ui.Paragraph(a.spListDetail(l), ui.ToneMuted))
	}
	if st.listErr != "" {
		rows = append(rows, ui.Paragraph(st.listErr, ui.ToneBad))
	}

	p := st.plan
	byCategory := st.mode == spByCategory && p != nil && len(p.Sounds) > 0
	ready := false // something to carry over
	switch {
	case p == nil:
		rows = append(rows, ui.Text("Reading your sound list…", "", ui.ToneMuted))
	case len(p.Sounds) == 0 && st.list != "":
		rows = append(rows, ui.Paragraph("No sounds Resound can play in this list.", ui.ToneMuted))
	case len(p.Sounds) > 0:
		rows = append(rows, ui.Section("Your sounds"),
			ui.Choices(
				ui.Choice{Icon: ui.IconCategory, Title: "By category",
					Text: "Copies your sounds into a folder per category, in Soundpad's order."},
				ui.Choice{Icon: ui.IconFolder, Title: "By folder",
					Text: "Adds the folders your sounds are in, as they are. Your categories don't come over."},
				int(st.mode), a.setSoundpadMode))
		if byCategory {
			rows, ready = a.spCategoryRows(rows, p)
		} else {
			rows, ready = a.spFolderRows(rows, p)
		}
	}

	if p != nil && (p.Favorites() > 0 || p.Colored() > 0) || !st.settings.Empty() || st.settings.MuteMic {
		rows = append(rows, ui.Section("Settings"))
	}
	if p != nil {
		need := ""
		if !byCategory && len(p.Sounds) > 0 && a.spReachable() < len(p.Sounds) {
			need = " Sounds in folders you don't add stay out."
		}
		if p.Favorites() > 0 {
			once := ""
			if byCategory {
				once = " A sound in several categories is a favorite once."
			}
			rows = append(rows, ui.Toggle("Hotbar sounds as favorites", a.spOn(spFavorites), func() { a.toggleSp(spFavorites) }),
				ui.Paragraph(plural(p.Favorites(), "sound")+" from your hotbar, also in the in-game overlay."+once+need, ui.ToneMuted))
		}
		if p.Colored() > 0 {
			rows = append(rows, ui.Toggle("Colors", a.spOn(spColors), func() { a.toggleSp(spColors) }),
				ui.Paragraph(plural(p.Colored(), "sound")+", as the nearest of the phone's colors."+need, ui.ToneMuted))
		}
	}
	if !st.settings.Empty() {
		rows = append(rows, ui.Toggle("Keys", a.spOn(spKeys), func() { a.toggleSp(spKeys) }), ui.Paragraph(a.spKeysDetail(), ui.ToneMuted))
	}
	if st.settings.MuteMic {
		rows = append(rows, ui.Toggle("Mute your mic while sounds play", a.spOn(spMuteMic), func() { a.toggleSp(spMuteMic) }),
			ui.Paragraph("As in Soundpad: your mic is muted while any sound plays, so others hear only the sound.", ui.ToneMuted))
	}
	for _, s := range st.settings.Skipped {
		rows = append(rows, ui.Paragraph(s, ui.ToneWarning))
	}
	if p != nil {
		rows = append(rows, ui.Spacer(4), ui.Paragraph(spNotCarried(p, byCategory), ui.ToneMuted))
	}
	if st.err != "" {
		rows = append(rows, ui.Paragraph(st.err, ui.ToneBad))
	}

	rows = append(rows, ui.Flex(), ui.Separator())
	if copying {
		files, _ := p.Copies()
		stop := st.copying
		return append(rows, ui.Text(fmt.Sprintf("Copying your sounds… %d of %d", st.copied, files), "", ui.ToneMuted),
			ui.Button("Stop copying", ui.ButtonNormal, false, func() { stop.Store(true) }),
			ui.Spacer(4))
	}
	label := "Import and continue"
	if byCategory {
		label = "Copy, import and continue" // the copy is needed for anything
	} else {
		ready = ready || a.spOn(spFavorites) || a.spOn(spColors) || a.spOn(spKeys) || a.spOn(spMuteMic)
	}
	return append(rows,
		ui.Button(label, ui.ButtonPrimary, p == nil || !ready, a.importSoundpad),
		ui.Button("Cancel", ui.ButtonNormal, false, a.closeSoundpad),
		ui.Spacer(4))
}

// spFolderRows are the folders to add by folder; ok reports whether one is
// chosen.
func (a *App) spFolderRows(rows []ui.Row, p *soundpad.Plan) (_ []ui.Row, ok bool) {
	rows = append(rows, ui.Section("Folders"))
	for _, f := range p.Folders {
		name := filepath.Base(f.Path)
		if a.folderCovering(f.Path) {
			rows = append(rows, ui.Text(name, "In your library", ui.ToneGood))
			continue
		}
		on := a.spFolderOn(f)
		ok = ok || on
		rows = append(rows, ui.ToggleDetail(name, spFolderCount(f), spFolderDetail(f), on, func() { a.toggleSpFolder(f) }))
	}
	return rows, ok
}

// spCategoryRows are the folders a copy by category makes and where they
// go; ok reports whether the copy can run.
func (a *App) spCategoryRows(rows []ui.Row, p *soundpad.Plan) (_ []ui.Row, ok bool) {
	st := &a.soundpad
	rows = append(rows, ui.Section("Categories"))
	const shown = 12
	for i, c := range p.Categories {
		if i == shown {
			rows = append(rows, ui.Paragraph(fmt.Sprintf("And %d more.", len(p.Categories)-shown), ui.ToneMuted))
			break
		}
		name := c.Title
		if c.Other {
			name += " (in no category)"
		}
		rows = append(rows, ui.Text(name, plural(len(c.Sounds), "sound"), ui.ToneMuted))
	}

	rows = append(rows, ui.Section("Copy to"))
	info, bad := a.spTarget()
	if st.target == "" {
		rows = append(rows, ui.Paragraph("Choose a new or empty folder. Resound adds it to your library, with a tab per category.", ui.ToneMuted),
			ui.Buttons(ui.Action{Label: "Choose a folder…", Icon: ui.IconFolderOpen, Closes: true, OnClick: a.chooseSoundpadTarget}))
	} else {
		rows = append(rows, ui.Text(filepath.Base(st.target), "", ui.ToneNormal), ui.Paragraph(st.target, ui.ToneMuted),
			ui.Buttons(ui.Action{Label: "Choose another folder…", Icon: ui.IconFolderOpen, Closes: true, OnClick: a.chooseSoundpadTarget}))
	}
	rows = append(rows, ui.Paragraph(info, ui.ToneWarning))
	if bad != "" {
		rows = append(rows, ui.Paragraph(bad, ui.ToneBad))
	}
	return rows, st.target != "" && bad == ""
}

// spListName names a list in the dropdown: by its file name, Soundpad's
// current list by the file it was saved as (the dropdown marks it current).
func (a *App) spListName(l *soundpad.List) string {
	name := l.Path
	if l.Current {
		if l.SavedAs == "" {
			return "Soundpad's list"
		}
		name = l.SavedAs
	}
	return strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
}

func (a *App) spListDetail(l *soundpad.List) string {
	var d []string
	if l.Current {
		d = append(d, "What Soundpad shows now")
	} else if cur := a.spCurrent(); cur != nil && strings.EqualFold(cur.SavedAs, l.Path) {
		d = append(d, "The current list's file, as last saved")
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

// spFolderCount is a folder's sounds, next to its name: "4 sounds", or
// "4 of 50 sounds" when it holds others too.
func spFolderCount(f soundpad.Folder) string {
	if f.Listed >= f.Files && !f.Many {
		return plural(f.Listed, "sound")
	}
	of := strconv.Itoa(f.Files)
	if f.Many {
		of += "+"
	}
	return fmt.Sprintf("%d of %s sounds", f.Listed, of)
}

// spFolderDetail is a folder's tooltip: where it is and what comes with it.
func spFolderDetail(f soundpad.Folder) string {
	if f.Listed >= f.Files && !f.Many {
		return f.Path + " · " + plural(f.Listed, "sound") + ", all in the list."
	}
	of := strconv.Itoa(f.Files)
	if f.Many {
		of = "more than " + of
	}
	return fmt.Sprintf("%s · %d of its %s sounds are in the list. Resound plays every sound in a folder you add, so the others come too.", f.Path, f.Listed, of)
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
func spNotCarried(p *soundpad.Plan, byCategory bool) string {
	d := []string{"your audio devices and volume (the next steps set them up)"}
	if n := len(p.Categories); !byCategory && n > 0 && !(n == 1 && p.Categories[0].Other) {
		d = append(d, "your categories and their order (Resound's tabs are your folders)")
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

// spSound is a file in the library that gets a Soundpad sound's settings.
type spSound struct {
	path     string
	sound    soundpad.Sound
	favorite bool // false for a sound's further copies
}

// spTab is a category's folder with its sounds in Soundpad's order.
type spTab struct {
	dir   string
	paths []string
}

// importSoundpad carries the chosen list over: by folder right away, by
// category after copying.
func (a *App) importSoundpad() {
	st := &a.soundpad
	p := st.plan
	if p == nil || st.copying != nil {
		return
	}
	st.err = ""
	if st.mode == spByCategory && len(p.Sounds) > 0 {
		if _, bad := a.spTarget(); st.target == "" || bad != "" {
			return
		}
		a.copySoundpad(p, st.target)
		return
	}
	var folders []string
	for _, f := range p.Folders {
		if a.spFolderOn(f) && !a.folderCovering(f.Path) {
			folders = append(folders, f.Path)
		}
	}
	a.carryOverSoundpad(folders, spOriginals(p), nil, nil)
}

// spOriginals are the plan's sounds where they are, to give settings to.
func spOriginals(p *soundpad.Plan) []spSound {
	sounds := make([]spSound, len(p.Sounds))
	for i, s := range p.Sounds {
		sounds[i] = spSound{s.Path, s, s.Hotbar}
	}
	return sounds
}

// copySoundpad copies the sounds by category off the UI thread, showing
// its progress on the page, and carries them over when done.
func (a *App) copySoundpad(p *soundpad.Plan, target string) {
	stop := &atomic.Bool{}
	a.soundpad.copying, a.soundpad.copied = stop, 0
	files, bytes := p.Copies()
	logx.Printf("soundpad: copying %d file(s), %d bytes, into %s", files, bytes, target)
	go func() {
		var last time.Time
		c, err := soundpad.CopyByCategory(target, p, func(done int) {
			if done < files && time.Since(last) < 100*time.Millisecond {
				return
			}
			last = time.Now()
			a.tray.Post(func() {
				if a.soundpad.copying == stop {
					a.soundpad.copied = done
					a.render()
				}
			})
		}, stop.Load)
		a.tray.Post(func() { a.copiedSoundpad(p, target, c, err) })
	}()
	a.render()
}

// copiedSoundpad carries a finished copy over, or says why it ended.
func (a *App) copiedSoundpad(p *soundpad.Plan, target string, c soundpad.Copied, err error) {
	st := &a.soundpad
	st.copying = nil
	logx.Printf("soundpad: copied %d file(s), %d there already, %d unreadable: %v", c.Copied, c.Existing, c.Failed, err)
	if err != nil {
		what := "Copying failed (" + err.Error() + ")"
		if errors.Is(err, soundpad.ErrStopped) {
			what = "Copying stopped"
		}
		st.err = what + ": nothing was carried over."
		if c.Copied > 0 {
			st.err += " The " + plural(c.Copied, "file") + " copied so far are in " + target + "; copying there again skips them."
		}
		a.render()
		return
	}
	sounds, tabs := spCopies(p, target, c)
	var note []string
	if c.Failed > 0 {
		note = append(note, plural(c.Failed, "sound")+" from Soundpad couldn't be read and stayed out.")
	}
	a.carryOverSoundpad([]string{target}, sounds, tabs, note)
}

// spCopies are the copies by category as sounds to give settings to, a
// sound's hotbar favorite only to its first copy, and as tabs in
// Soundpad's order.
func spCopies(p *soundpad.Plan, target string, c soundpad.Copied) (sounds []spSound, tabs []spTab) {
	favorite := map[int]bool{} // sounds made a favorite through a copy already
	for ci, cat := range p.Categories {
		t := spTab{dir: filepath.Join(target, cat.Dir)}
		for k, i := range cat.Sounds {
			path := c.Paths[ci][k]
			if path == "" {
				continue
			}
			s := p.Sounds[i]
			sounds = append(sounds, spSound{path, s, s.Hotbar && !favorite[i]})
			favorite[i] = favorite[i] || s.Hotbar
			t.paths = append(t.paths, path)
		}
		tabs = append(tabs, t)
	}
	return sounds, tabs
}

// carryOverSoundpad adds the folders, carries the chosen parts over and
// continues with the setup's first step.
func (a *App) carryOverSoundpad(folders []string, sounds []spSound, tabs []spTab, note []string) {
	if err := a.beginCarryOver("soundpad"); err != "" {
		a.soundpad.err = err
		a.render()
		return
	}
	known := slices.ContainsFunc(folders, a.folderCovering)
	note = append(a.carryOverFolders(folders), note...)
	if known && len(tabs) > 0 {
		a.rescan() // copied into a library folder, which addFolders doesn't scan again
	}
	r := a.applySoundpad(sounds)
	for _, t := range tabs {
		if a.orderTab(t.dir, t.paths) {
			r.ordered++
		}
	}
	if r.favorites+r.colors > 0 {
		note = append(note, fmt.Sprintf("From Soundpad: %s, %s.", plural(r.favorites, "favorite"), plural(r.colors, "color")))
	}
	logx.Printf("soundpad: imported: %d folder(s), %d favorite(s), %d color(s), %d tab(s) in Soundpad's order, %d key(s), mute mic %v",
		len(folders), r.favorites, r.colors, r.ordered, r.keys, r.muteMic)
	for _, n := range r.notes {
		logx.Printf("soundpad: %s", n)
	}
	a.soundpad = soundpadState{read: true} // carried over: not offered again
	a.finishCarryOver(append(note, r.notes...))
}

// spResult is what applySoundpad and the tab orders carried over.
type spResult struct {
	favorites, colors, ordered, keys int
	muteMic                          bool
	notes                            []string
}

// applySoundpad carries the chosen parts over to sounds in the library
// (folders are added before). Like applySoundux it only fills what Resound
// doesn't have: a favorite is added, a color only where the sound has
// none, keys are added to the ones set. Nothing is saved or applied here.
func (a *App) applySoundpad(sounds []spSound) spResult {
	var r spResult
	if a.spOn(spFavorites) || a.spOn(spColors) {
		for _, s := range sounds {
			id, ok := a.lib.FindPath(s.path)
			if !ok {
				continue // its folder wasn't added
			}
			fav, color := false, false
			_, _, err := a.lib.Update(id, func(st *library.SoundSettings) {
				if a.spOn(spFavorites) && s.favorite && !st.Favorite {
					st.Favorite, fav = true, true
				}
				if a.spOn(spColors) && s.sound.Color != "" && st.Color == "" {
					st.Color, color = s.sound.Color, true
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
	if a.spOn(spMuteMic) {
		// Soundpad muted the mic for every sound: "Always".
		a.cfg.MuteMicWhilePlaying, a.cfg.MuteMicMinSeconds, r.muteMic = true, 0, true
	}
	return r
}

// phoneLayouts are the phone's layout modes, each with a custom order of
// its own (library.Layout).
var phoneLayouts = []string{"grid-3", "grid-2", "list"}

// orderTab gives the tab of dir a custom order of paths in every layout,
// unless the user arranged or sorted it on the phone already. It reports
// whether it did.
func (a *App) orderTab(dir string, paths []string) bool {
	var ids []uint64
	for _, p := range paths {
		if id, ok := a.lib.FindPath(p); ok && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return false
	}
	for _, t := range a.lib.Tabs() {
		if !strings.EqualFold(filepath.Clean(t.Path), filepath.Clean(dir)) {
			continue
		}
		set := false
		_, err := a.lib.UpdateLayout(strconv.FormatUint(t.ID, 10), func(l *library.Layout) {
			if l.Sort == "" && len(l.Orders) == 0 {
				l.Orders = map[string][]uint64{}
				for _, mode := range phoneLayouts {
					l.Orders[mode] = slices.Clone(ids)
				}
				set = true
			}
		})
		if err != nil {
			logx.Printf("soundpad: saving a tab's order: %v", err)
		}
		return set && err == nil
	}
	return false
}

// nonNil is a single combination as a list, none for nil.
func nonNil(k []uint16) [][]uint16 {
	if k == nil {
		return nil
	}
	return [][]uint16{k}
}
