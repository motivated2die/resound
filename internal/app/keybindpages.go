package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/ui"
)

// The Keybinds page: the global keys of each function as cards, then the
// apps with keybinds of their own (each opens a page with the same cards
// for that app) and a list of running apps to add one, then two options.
// Texts stay short: a line or two under each section; the info icons and
// the help article say more.

func (a *App) keysSummary() (string, ui.Tone) {
	if keys := a.globalKeys(fnPTT); len(keys) > 0 {
		return keysText(keys), ui.ToneGood
	}
	if a.keys.Hotkeys() {
		return "No push-to-talk", ui.ToneMuted
	}
	return "Not set", ui.ToneMuted
}

func (a *App) keysPage() []ui.Row {
	rows := []ui.Row{
		ui.HeaderButton("Keybinds", a.back, ui.IconHelp, "Help", "How keybinds work: opens the help article", func() { a.openHelpAt(helpKeybinds) }),
		ui.Section("Global"),
		ui.Paragraph("Keys and mouse buttons that work in every game and app, unless an app below has its own. Click + to add one.", ui.ToneMuted),
	}
	rows = append(rows, a.keybindCards("")...)
	rows = append(rows, a.appListRows()...)
	rows = append(rows, a.keyOptionRows()...)
	return append(rows, ui.Spacer(4))
}

// keybindCards are the cards of every function in one scope.
func (a *App) keybindCards(app string) []ui.Row {
	var rows []ui.Row
	for _, fn := range appKeyFuncs {
		t := keyTarget{app, fn}
		rows = append(rows, a.keybindCard(t))
	}
	return rows
}

// keyInfo is a function's info tooltip.
func keyInfo(fn keyFunc) string {
	switch fn {
	case fnPTT:
		return "Resound holds these keys while sounds play, so your game or Discord sends the sounds. " +
			"Use the keys you bound to push-to-talk there; several are held together."
	case fnPause:
		return "While you hold one, sounds pause and Resound lets go of push-to-talk and your mic, so you can talk, e.g. on another channel. " +
			"Letting go resumes them. The game still gets the key."
	case fnStop:
		return "Pressing one stops all sounds at once. The game still gets the key."
	}
	return "Opens the overlay in a game; again plays the highlighted sound."
}

func keyIcon(fn keyFunc) ui.Icon {
	return [...]ui.Icon{ui.IconMic, ui.IconPause, ui.IconStop, ui.IconKeyboard}[fn]
}

// keybindCard is the card of t: its keys (an app's own, or the global ones
// it keeps, muted), the tools to change them, and while recording for it,
// what was pressed and the ready-made keys.
func (a *App) keybindCard(t keyTarget) ui.Row {
	kb := a.keybind(t)
	p := &a.ptt
	s := ui.KeybindSpec{
		ID: fmt.Sprintf("keys-%s-%d", t.app, t.fn), Icon: keyIcon(t.fn), Title: t.fn.title(), Info: keyInfo(t.fn),
		OffLabel: "None: off in this app", AddTip: "Add a key or mouse button",
	}
	for _, k := range kb.keys {
		s.Keys = append(s.Keys, input.Combo(k).String())
	}
	switch {
	case t.app == "":
	case kb.off:
		s.Off, s.OnReset = true, func() { a.resetKeys(t) }
	case !kb.own:
		s.Inherited, s.Scope = true, "Global"
		s.AddTip = "Give " + a.appName(t.app) + " its own keys: they replace the global ones there"
	default:
		s.OnReset, s.ResetTip = func() { a.resetKeys(t) }, "Use the global keys again"
	}
	if kb.own && !kb.off {
		s.OnRemove = func(i int) { a.removeKeys(t, i) }
	}
	full := kb.own && !kb.off && len(kb.keys) >= t.fn.maxKeys()
	if !p.capturing && !full {
		s.OnAdd = func() { a.startCapture(t) }
	}
	if t.app != "" && !kb.off && !p.capturing {
		s.OnOff, s.OffTip = func() { a.setKeysOff(t) }, "Turn "+strings.ToLower(t.fn.title())+" off in "+a.appName(t.app)
	}
	if t.fn == fnPTT && len(kb.keys) > 0 {
		s.Tool, s.ToolTip = ui.IconKeyboard, fmt.Sprintf("Send to game in %d s: Resound presses these keys while you click the game's keybind field", pttSendDelay)
		if a.ptt.sendIn == 0 {
			s.OnTool = func() { a.sendPTTToGame(t) }
		}
	}
	if p.capturing && p.target == t {
		s.Recording, s.OnCancel = true, a.cancelCapture
		if len(p.captured) > 0 {
			s.Pressed = p.captured.String()
		}
		s.Hint = "Press a key, a combination or mouse button 3, 4 or 5 over this panel. Esc cancels."
		s.Presets, s.OnPreset = keyPresets(t), func(i int) { a.choosePreset(t, i) }
	}
	switch {
	case a.ptt.sendIn > 0 && a.ptt.sendFor == t:
		s.Note, s.NoteTone = fmt.Sprintf("Sending in %d… click the game's keybind field now.", a.ptt.sendIn), ui.ToneWarning
	case p.note != "" && p.noteFor == t:
		s.Note, s.NoteTone = p.note, ui.ToneWarning
	case a.hotkeyErr != "" && t.fn != fnPTT && len(kb.keys) > 0:
		s.Note, s.NoteTone = a.hotkeyErr, ui.ToneBad
	}
	return ui.Keybind(s)
}

// appListRows are the apps with keybinds of their own, each opening its
// page, and the running apps to add one from.
func (a *App) appListRows() []ui.Row {
	rows := []ui.Row{
		ui.Section("App keybinds"),
		ui.Paragraph("Give a game keys of its own. They replace the global ones while it's in front; functions you leave alone keep the global keys.", ui.ToneMuted),
	}
	for i := range a.cfg.AppKeybinds {
		e := &a.cfg.AppKeybinds[i]
		detail, tone := overrideSummary(e), ui.ToneMuted
		if hasOverrides(e) {
			tone = ui.ToneGood
		}
		exe := e.Exe
		rows = append(rows, ui.Nav(a.appName(exe), detail, tone, func() { a.openAppKeys(exe) }))
	}
	var opts []ui.Option
	var apps []input.App
	for _, app := range a.windowApps {
		if a.appKeybinds(app.Exe) == nil {
			opts = append(opts, ui.Option{Label: app.Name, Detail: app.Exe})
			apps = append(apps, app)
		}
	}
	placeholder := "Add a running app…"
	if len(opts) == 0 {
		placeholder = "Start a game to add it here"
	}
	return append(rows, ui.Dropdown("add-app", "", placeholder, opts, -1, func(i int) { a.addApp(apps[i]) }))
}

// keyOptionRows are the switches for push-to-talk while sounds play.
func (a *App) keyOptionRows() []ui.Row {
	rows := []ui.Row{
		ui.Section("Options"),
		ui.ToggleInfo("Hold push-to-talk while sounds play",
			"Off: Resound never presses push-to-talk for sounds. Talk-through on the phone always holds it.",
			a.cfg.PushToTalkWhilePlaying, a.togglePTTWhilePlaying),
	}
	if a.anyPTT() && a.keys.Hotkeys() {
		rows = append(rows, ui.ToggleInfo("Let go of push-to-talk first",
			"While sounds hold push-to-talk, a pause or stop key waits a moment for Resound to let go of it, "+
				"so the game doesn't end your own push-to-talk too. Turn this off if a game ignores your key while sounds play.",
			!a.cfg.NoPushToTalkHandOver, a.togglePTTHandOver))
	}
	return rows
}

// anyPTT reports whether push-to-talk keys are set anywhere.
func (a *App) anyPTT() bool {
	if len(a.cfg.PushToTalkKeys) > 0 {
		return true
	}
	return slices.ContainsFunc(a.cfg.AppKeybinds, func(e config.AppKeybinds) bool { return e.PushToTalk != nil && len(e.PushToTalk.Keys) > 0 })
}

// openAppKeys shows an app's keybinds page.
func (a *App) openAppKeys(exe string) {
	a.keysApp, a.removingApp = exe, false
	if a.page == pageAppKeys {
		a.render()
		return
	}
	a.openPage(pageAppKeys)
}

// appKeysPage is one app's keybinds: the cards of each function for it,
// and removing it.
func (a *App) appKeysPage() []ui.Row {
	exe := a.keysApp
	if a.appKeybinds(exe) == nil {
		return a.keysPage()
	}
	state := "Not running"
	if a.running[exe] {
		state = "Running"
	}
	rows := []ui.Row{
		ui.HeaderTool(a.appName(exe), a.back, ui.IconTrash, "Remove this app's keybinds", func() { a.removingApp = true; a.render() }),
		ui.Text(exe, state, ui.ToneMuted),
	}
	if a.removingApp {
		rows = append(rows,
			ui.Paragraph("Remove the keybinds for "+a.appName(exe)+"? The global keys apply there again.", ui.ToneWarning),
			ui.Buttons(
				ui.Action{Label: "Cancel", OnClick: func() { a.removingApp = false; a.render() }},
				ui.Action{Label: "Remove", Style: ui.ButtonDanger, OnClick: func() { a.removeApp(exe) }},
			),
			ui.Separator())
	}
	rows = append(rows,
		ui.Section("While it is in the foreground"),
		ui.Paragraph("Each function keeps the global keys until you add its own. ⊘ turns it off here, ↺ brings the global keys back.", ui.ToneMuted))
	rows = append(rows, a.keybindCards(exe)...)
	return append(rows, ui.Spacer(4))
}

// refreshWindowApps lists, off the UI thread, the programs with a window,
// for adding one on the Keybinds page.
func (a *App) refreshWindowApps() {
	go func() {
		apps := input.WindowApps()
		a.tray.Post(func() {
			a.windowApps = apps
			if a.page == pageKeys {
				a.render()
			}
		})
	}()
}
