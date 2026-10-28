package app

import (
	"slices"
	"strings"

	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// The in-game overlay: one key slides a bar of the favorites in at the edge
// of the primary screen; the mouse wheel moves the highlight, the key again
// plays the highlighted sound, Esc closes it. Holding Esc for
// input.EscHoldTime closes it and stops all sounds; the bar turns red
// meanwhile. It never takes the focus from
// the game, and only its key, Esc and the wheel are kept from the game
// while it is open (input.Filter decides that in the keyboard hook). It is
// on while a key is set for it: the global one, or an app's own while that
// app is in the foreground (keybinds.go), which can also turn it off there.
//
// The highlight comes back where it was left (cfg.OverlayLast), so a sound
// can be picked ahead and played later with two presses; the very first
// time it starts in the middle.

const overlayEmptyText = "Star sounds on your phone to see them here"

// overlayState is the overlay's UI-thread state.
type overlayState struct {
	win     *ui.Overlay // created on first use
	err     string      // why it can't be shown, if it can't
	open    bool
	preview bool     // opened from the panel's button rather than the key
	ids     []uint64 // the sounds shown, top to bottom
	sel     int
}

// overlayFavorites returns the favorites in the order of the phone's
// Favorites tab (its custom order for its current layout, the rest after
// it, or by name when the phone sorts it so), as overlay items.
func (a *App) overlayFavorites() ([]uint64, []ui.OverlayItem) {
	favs, _ := a.lib.ShownSounds(library.FavoritesKey)
	ids := make([]uint64, len(favs))
	items := make([]ui.OverlayItem, len(favs))
	for i, s := range favs {
		ids[i] = s.ID
		items[i] = ui.OverlayItem{Name: s.Name, Emoji: s.Emoji, Color: s.Color}
	}
	return ids, items
}

// overlaySelection is where the highlight starts: the sound highlighted
// last, else the middle one.
func (a *App) overlaySelection(ids []uint64) int {
	if i := slices.Index(ids, a.cfg.OverlayLast); i >= 0 && a.cfg.OverlayLast != 0 {
		return i
	}
	return (len(ids) - 1) / 2
}

// openOverlay slides the overlay in. It reports false when it can't show.
func (a *App) openOverlay() bool {
	if a.overlay.win == nil {
		w, err := ui.NewOverlay()
		if err != nil {
			logx.Printf("overlay: %v", err)
			a.overlay.err = "The overlay can't be shown on this PC: " + err.Error()
			return false
		}
		a.overlay.win, a.overlay.err = w, ""
	}
	ids, items := a.overlayFavorites()
	o := &a.overlay
	o.ids, o.sel, o.open = ids, a.overlaySelection(ids), true
	o.win.Show(items, o.sel, !a.cfg.OverlayRight, overlayEmptyText)
	return true
}

// closeOverlay slides it out and remembers the highlight.
func (a *App) closeOverlay() {
	o := &a.overlay
	if !o.open {
		return
	}
	o.open = false
	a.rememberOverlaySelection()
	if o.preview {
		o.preview = false
		a.applyHotkeys() // the watcher may only have run for the preview
		a.render()
	}
	if o.win != nil {
		o.win.Hide(nil)
	}
}

// closeOverlayWithoutKey closes the overlay when the app in front has no key
// for it any more (removed, turned off there, or another app came to the
// front), as then only Esc could close it.
func (a *App) closeOverlayWithoutKey() {
	if a.overlay.open && !a.overlay.preview && len(a.keysFor(a.fgApp, fnOverlay)) == 0 {
		logx.Printf("overlay: no key for it in front, closing")
		a.closeOverlay()
		a.tellHookOverlay(false)
	}
}

func (a *App) rememberOverlaySelection() {
	o := &a.overlay
	if o.sel >= 0 && o.sel < len(o.ids) && o.ids[o.sel] != a.cfg.OverlayLast {
		a.cfg.OverlayLast = o.ids[o.sel]
		a.save()
	}
}

// onOverlayEvent handles the overlay's hotkey events; it reports false for
// other events. UI thread.
func (a *App) onOverlayEvent(ev input.HotkeyEvent) bool {
	o := &a.overlay
	switch ev.Action {
	case input.ActionOverlayOpen:
		if a.ptt.capturing || !a.openOverlay() {
			a.tellHookOverlay(false) // or Esc and the wheel would stay taken
		}
	case input.ActionOverlayScroll:
		if o.open && len(o.ids) > 0 {
			o.sel = max(0, min(len(o.ids)-1, o.sel+ev.Steps)) // stops at the ends
			o.win.SetSelected(o.sel)
		}
	case input.ActionOverlaySelect:
		a.selectOverlay()
	case input.ActionOverlayEscDown:
		if o.open {
			o.win.HoldToStop(input.EscHoldTime)
		}
	case input.ActionOverlayClose:
		a.closeOverlay()
	case input.ActionOverlayStop:
		logx.Printf("overlay: Esc held, stopping all sounds")
		a.stopAll()
		a.closeOverlay()
	default:
		return false
	}
	return true
}

// selectOverlay plays the highlighted sound; the highlight flashes green,
// or red when it can't play, and the overlay slides out.
func (a *App) selectOverlay() {
	o := &a.overlay
	if !o.open {
		return
	}
	if o.sel < 0 || o.sel >= len(o.ids) {
		a.closeOverlay()
		return
	}
	id := o.ids[o.sel]
	o.open = false
	a.rememberOverlaySelection()
	if o.preview {
		o.preview = false
		a.applyHotkeys()
		a.render()
	}
	go func() { // decodes off the UI thread, like a play from the phone
		_, err := a.playSound(id, false)
		if err != nil {
			logx.Printf("overlay: playing sound %d: %v", id, err)
		}
		a.tray.Post(func() { o.win.Confirm(err == nil) })
	}()
}

// tellHookOverlay tells the keyboard hook that the overlay opened or closed
// without its key (the preview button), so Esc and the wheel are taken
// exactly while it shows.
func (a *App) tellHookOverlay(open bool) {
	if a.hotkeys != nil {
		a.hotkeys.SetOverlayOpen(open)
	}
}

// toggleOverlayPreview shows the overlay from the panel, to see what it
// looks like without a game. Esc and the wheel work as in a game.
func (a *App) toggleOverlayPreview() {
	o := &a.overlay
	if o.open {
		a.closeOverlay()
		a.tellHookOverlay(false)
		return
	}
	o.preview = true
	a.applyHotkeys() // starts the watcher for Esc and the wheel if no key is set
	if !a.openOverlay() {
		o.preview = false
		a.applyHotkeys()
		a.render()
		return
	}
	a.tellHookOverlay(true)
	a.render()
}

func (a *App) setOverlaySide(left bool) {
	a.cfg.OverlayRight = !left
	a.save()
	if a.overlay.open { // show it on the new side at once
		ids, items := a.overlayFavorites()
		a.overlay.ids = ids
		a.overlay.sel = max(0, min(a.overlay.sel, len(ids)-1))
		a.overlay.win.Hide(func() {
			if a.overlay.open {
				a.overlay.win.Show(items, a.overlay.sel, !a.cfg.OverlayRight, overlayEmptyText)
			}
		})
	}
	a.render()
}

// --- panel ------------------------------------------------------------------

func (a *App) overlayKeyText() string { return input.Combo(a.cfg.OverlayKeys).String() }

// overlayApps are the apps with an overlay setting of their own: a key, or
// the overlay turned off there.
func (a *App) overlayApps() []string {
	var exes []string
	for _, e := range a.cfg.AppKeybinds {
		if e.Overlay != nil {
			exes = append(exes, e.Exe)
		}
	}
	return exes
}

// overlayUsed reports whether the overlay has a key anywhere.
func (a *App) overlayUsed() bool {
	return len(a.cfg.OverlayKeys) > 0 || slices.ContainsFunc(a.cfg.AppKeybinds, func(e config.AppKeybinds) bool {
		return e.Overlay != nil && len(e.Overlay.Keys) > 0
	})
}

func (a *App) overlaySummary() (string, ui.Tone) {
	switch {
	case len(a.cfg.OverlayKeys) > 0:
		return a.overlayKeyText(), ui.ToneGood
	case a.overlayUsed():
		return "In some apps", ui.ToneGood
	}
	return "Off", ui.ToneMuted
}

// overlayAppsRow is the note under the global key: apps can have their own
// key or none, and which do, with a link to the app keybinds.
func (a *App) overlayAppsRow() ui.Row {
	text := "A game can have an overlay key of its own, or no overlay at all."
	if exes := a.overlayApps(); len(exes) > 0 {
		parts := make([]string, len(exes))
		for i, exe := range exes {
			state := "off"
			if kb := a.keybind(keyTarget{exe, fnOverlay}); !kb.off {
				state = keysText(kb.keys)
			}
			parts[i] = a.appName(exe) + " (" + state + ")"
		}
		text = "Own setting in " + strings.Join(parts, ", ") + ": it replaces this key while that app is in front."
	}
	return ui.ParagraphLink(text, "Set it in App keybinds", "Opens the Keybinds page with the apps that have keys of their own",
		func() { a.openPage(pageKeys) })
}

// overlayPreviewRow is the small picture of the overlay (always the same
// examples) with what it does beside it, and further paragraphs below that.
func (a *App) overlayPreviewRow(more ...string) ui.Row {
	text, key := "Bring up your favorite sounds in any game with one key, without leaving the game.", ""
	if len(a.cfg.OverlayKeys) > 0 {
		key = a.overlayKeyText()
		text = "Press " + key + " in a game, scroll to a favorite and press " + key + " again to play it."
	}
	return ui.OverlayPreview(!a.cfg.OverlayRight, key, append([]string{text}, more...)...)
}

func (a *App) overlayPage() []ui.Row {
	rows := []ui.Row{
		ui.Header("In-game overlay", a.back),
		a.overlayPreviewRow(
			"Esc closes the bar. Hold Esc to also stop all sounds.",
			"Your game keeps the focus: only the key, Esc and the mouse wheel are kept from it while the bar shows."),
		ui.Section("Key"),
		a.keybindCard(keyTarget{fn: fnOverlay}),
		a.overlayAppsRow(),
	}
	rows = append(rows,
		ui.Section("Screen edge"),
		// Greyed out while the overlay is off (no key anywhere).
		ui.Segmented([]string{"Left", "Right"}, map[bool]int{false: 0, true: 1}[a.cfg.OverlayRight],
			!a.overlayUsed(), func(i int) { a.setOverlaySide(i == 0) }),
		ui.Section("Try it"),
		ui.Paragraph("See the bar with your favorites on this screen, no game needed. It works as in a game: scroll through it, press your key to play a sound, Esc to close it.", ui.ToneMuted),
	)
	label := "Show the overlay"
	if a.overlay.open {
		label = "Hide the overlay"
	}
	rows = append(rows, ui.Button(label, ui.ButtonNormal, false, a.toggleOverlayPreview))
	if a.overlay.err != "" {
		rows = append(rows, ui.Paragraph(a.overlay.err, ui.ToneBad))
	}
	return append(rows, ui.Spacer(4))
}
