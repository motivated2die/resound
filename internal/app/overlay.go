package app

import (
	"slices"

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
// on while a key is set for it.
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
	favs := a.lib.Favorites()
	lay := a.lib.Layout(library.FavoritesKey)
	mode := lay.Mode
	if mode == "" {
		mode = "grid-3" // the phone's default
	}
	pos := map[uint64]int{}
	for i, id := range lay.Orders[mode] {
		if _, dup := pos[id]; !dup {
			pos[id] = i
		}
	}
	slices.SortStableFunc(favs, func(x, y library.Sound) int {
		px, okx := pos[x.ID]
		py, oky := pos[y.ID]
		switch {
		case okx && oky:
			return px - py
		case okx:
			return -1
		case oky:
			return 1
		}
		return 0
	})
	switch lay.Sort {
	case library.SortNameAsc:
		slices.SortStableFunc(favs, func(x, y library.Sound) int { return library.CompareNames(x.Name, y.Name) })
	case library.SortNameDesc:
		slices.SortStableFunc(favs, func(x, y library.Sound) int { return library.CompareNames(y.Name, x.Name) })
	}
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
	o.win.Hide(nil)
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

func (a *App) overlaySummary() (string, ui.Tone) {
	if len(a.cfg.OverlayKeys) == 0 {
		return "Off", ui.ToneMuted
	}
	return a.overlayKeyText(), ui.ToneGood
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
	}
	rows = append(rows,
		ui.Section("Screen edge"),
		// Greyed out while the overlay is off (no key).
		ui.Segmented([]string{"Left", "Right"}, map[bool]int{false: 0, true: 1}[a.cfg.OverlayRight],
			len(a.cfg.OverlayKeys) == 0, func(i int) { a.setOverlaySide(i == 0) }),
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
