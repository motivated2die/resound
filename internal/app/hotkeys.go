package app

import (
	"slices"

	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// Hotkeys work anywhere in Windows, e.g. in a game: holding a pause key
// pauses the sounds and lets go of push-to-talk and the mic, so the user can
// talk on another channel; a stop key stops everything. The keyboard is only
// watched while at least one hotkey is set (input.Watcher).

// maxHotkeys limits each list; more would not fit the panel.
const maxHotkeys = 8

// captureTarget is what a key recorded in the panel is for.
type captureTarget int

const (
	capturePTT captureTarget = iota
	capturePause
	captureStop
	captureOverlay
)

func (a *App) hotkeyBindings() input.Bindings {
	var b input.Bindings
	for _, c := range a.cfg.PauseHotkeys {
		b.Pause = append(b.Pause, input.Combo(c))
	}
	for _, c := range a.cfg.StopHotkeys {
		b.Stop = append(b.Stop, input.Combo(c))
	}
	// While a key is recorded in the panel, the overlay key isn't taken from
	// it, so it can be recorded again.
	if !a.ptt.capturing {
		b.Overlay = input.Combo(a.cfg.OverlayKeys)
	}
	return b
}

// applyHotkeys starts, updates or stops watching the keyboard to match the
// settings. The overlay's preview needs the watcher too (Esc and the wheel).
func (a *App) applyHotkeys() {
	b := a.hotkeyBindings()
	a.setHoldPause(false) // a held pause key may no longer be one
	switch {
	case b.Empty() && !a.overlay.preview:
		a.closeHotkeys()
		a.hotkeyErr = ""
	case a.hotkeys != nil:
		a.hotkeys.SetBindings(b)
	default:
		w, err := input.StartWatcher(b, func(ev input.HotkeyEvent) {
			a.tray.Post(func() { a.onHotkey(ev) }) // the hook must not wait
		})
		if err != nil {
			logx.Printf("hotkeys: %v", err)
			a.hotkeyErr = "Hotkeys don't work right now: " + err.Error()
			return
		}
		a.hotkeys, a.hotkeyErr = w, ""
	}
	if a.hotkeys != nil {
		logx.Printf("hotkeys: watching %d pause and %d stop key(s), overlay key %q", len(b.Pause), len(b.Stop), b.Overlay.String())
	}
}

func (a *App) closeHotkeys() {
	if a.hotkeys != nil {
		a.hotkeys.Close()
		a.hotkeys = nil
		logx.Printf("hotkeys: off")
	}
}

// onHotkey runs on the UI thread.
func (a *App) onHotkey(ev input.HotkeyEvent) {
	if a.onOverlayEvent(ev) {
		return
	}
	if ev.Down && a.ptt.capturing {
		return // the user is recording a key in the panel
	}
	switch ev.Action {
	case input.ActionStop:
		logx.Printf("hotkey: stop")
		a.stopAll()
	case input.ActionPause:
		a.setHoldPause(ev.Down)
	}
	a.render()
}

func (a *App) hotkeyList(t captureTarget) *[][]uint16 {
	if t == captureStop {
		return &a.cfg.StopHotkeys
	}
	return &a.cfg.PauseHotkeys
}

// addHotkey adds a recorded combination to a list, unless it is already
// in use.
func (a *App) addHotkey(t captureTarget, c input.Combo) {
	a.ptt.hotkeyNote, a.ptt.noteFor = "", t
	if t == captureOverlay {
		a.setOverlayKey(c)
		return
	}
	list := a.hotkeyList(t)
	other, otherName := a.cfg.StopHotkeys, "stop"
	if t == captureStop {
		other, otherName = a.cfg.PauseHotkeys, "pause"
	}
	switch {
	case input.SameCombo(c, a.cfg.OverlayKeys):
		a.ptt.hotkeyNote = c.String() + " opens the in-game overlay."
	case containsCombo(*list, c):
		a.ptt.hotkeyNote = c.String() + " is already in this list."
	case containsCombo(other, c):
		a.ptt.hotkeyNote = c.String() + " is already a " + otherName + " key."
	case input.SameCombo(c, a.cfg.PushToTalkKeys):
		a.ptt.hotkeyNote = c.String() + " is the push-to-talk key Resound presses."
	case len(*list) >= maxHotkeys:
		a.ptt.hotkeyNote = "That's the most this list can hold."
	default:
		*list = append(*list, slices.Clone(c))
		a.save()
		logx.Printf("hotkeys: %s key %s added", map[captureTarget]string{capturePause: "pause", captureStop: "stop"}[t], c)
		a.applyHotkeys()
	}
	a.render()
}

func containsCombo(list [][]uint16, c input.Combo) bool {
	for _, k := range list {
		if input.SameCombo(k, c) {
			return true
		}
	}
	return false
}

func (a *App) clearHotkeys(t captureTarget) {
	*a.hotkeyList(t) = nil
	a.ptt.hotkeyNote = ""
	a.save()
	a.applyHotkeys()
	a.render()
}

// hotkeyRows is the pause or stop section of the push-to-talk page.
func (a *App) hotkeyRows(t captureTarget) []ui.Row {
	title, intro := "Hold to pause",
		"While you hold one of these keys, sounds pause and Resound lets go of push-to-talk and your mic, so you can talk on another channel. Letting go resumes them."
	if t == captureStop {
		title, intro = "Stop keys", "Pressing one of these keys stops all sounds at once and lets go of push-to-talk."
	}
	keys := *a.hotkeyList(t)
	rows := []ui.Row{ui.Section(title), ui.Paragraph(intro, ui.ToneMuted)}
	for _, k := range keys {
		rows = append(rows, ui.Text(input.Combo(k).String(), "", ui.ToneGood))
	}
	if a.ptt.capturing && a.ptt.target == t {
		got := "…"
		if len(a.ptt.captured) > 0 {
			got = a.ptt.captured.String() + " …"
		}
		return append(rows,
			ui.Text("Press the key or combination", got, ui.ToneWarning),
			ui.Text("Keyboard keys only. Esc cancels.", "", ui.ToneMuted))
	}
	if len(keys) == 0 {
		rows = append(rows, ui.Text("No keys yet", "", ui.ToneMuted))
	}
	rows = append(rows, ui.Buttons(
		ui.Action{Label: "Add key", Disabled: a.ptt.capturing || len(keys) >= maxHotkeys, OnClick: func() { a.startCapture(t) }},
		ui.Action{Label: "Clear keys", Disabled: len(keys) == 0, OnClick: func() { a.clearHotkeys(t) }},
	))
	if a.ptt.hotkeyNote != "" && a.ptt.noteFor == t {
		rows = append(rows, ui.Paragraph(a.ptt.hotkeyNote, ui.ToneWarning))
	}
	if a.hotkeyErr != "" && len(keys) > 0 {
		rows = append(rows, ui.Paragraph(a.hotkeyErr, ui.ToneBad))
	}
	return rows
}
