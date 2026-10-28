package app

import (
	"maps"
	"slices"

	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/logx"
)

// Hotkeys work anywhere in Windows, e.g. in a game: holding a pause key
// pauses the sounds and lets go of push-to-talk and the mic, so the user can
// talk on another channel; a stop key stops everything. Keys and mouse
// buttons 3–5 work, alone or together. The keyboard (and the mouse, while a
// binding holds a button) is only watched while a hotkey is set or an app
// has keybinds of its own (input.Watcher); the watcher then also reports
// which app is in the foreground, whose keybinds apply (keybinds.go).

// keymap is the bindings the watcher matches: the global ones, and for each
// app that replaces some of them, its complete set.
func (a *App) keymap() input.Keymap {
	k := input.Keymap{Global: a.bindingsFor("")}
	for i := range a.cfg.AppKeybinds {
		if e := &a.cfg.AppKeybinds[i]; hasOverrides(e) {
			if k.Apps == nil {
				k.Apps = map[string]input.Bindings{}
			}
			k.Apps[e.Exe] = a.bindingsFor(e.Exe)
		}
	}
	return k
}

// bindingsFor is what pause, stop and the overlay's and launchpad's keys
// are while app is in the foreground. While a key is recorded in the panel,
// the overlay and launchpad keys aren't taken from it, so they can be
// recorded again. Nor does the user's
// push-to-talk count then. The overlay's preview in the panel plays with
// the global key its page names, whatever app was in front before.
func (a *App) bindingsFor(app string) input.Bindings {
	b := input.Bindings{Pause: combos(a.keysFor(app, fnPause)), Stop: combos(a.keysFor(app, fnStop))}
	if !a.ptt.capturing {
		overlay := a.keysFor(app, fnOverlay)
		if a.overlay.preview {
			overlay = a.globalKeys(fnOverlay)
		}
		if len(overlay) > 0 {
			b.Overlay = input.Combo(overlay[0])
		}
		if keys := a.keysFor(app, fnLaunchpad); len(keys) > 0 {
			b.Launchpad = input.Combo(keys[0])
		}
		if a.talkPauses() {
			b.Talk = combos(a.keysFor(app, fnPTT)) // the user's own presses pause (talk.go)
		}
	}
	return b
}

// applyHotkeys starts, updates or stops watching the keyboard to match the
// settings. The overlay's preview needs the watcher too (Esc and the wheel).
func (a *App) applyHotkeys() {
	k := a.keymap()
	a.setHoldPause(false) // a held pause key may no longer be one
	switch {
	case k.Empty() && !a.overlay.preview:
		a.closeHotkeys()
		a.hotkeyErr = ""
		return
	case a.hotkeys != nil:
		a.hotkeys.SetKeymap(k)
	default:
		w, err := input.StartWatcher(k, func(ev input.HotkeyEvent) {
			a.tray.Post(func() { a.onHotkey(ev) }) // the hook must not wait
		})
		if err != nil {
			logx.Printf("hotkeys: %v", err)
			a.hotkeyErr = "Hotkeys don't work right now: " + err.Error()
			return
		}
		a.hotkeys, a.hotkeyErr = w, ""
	}
	if !sameKeymap(k, a.keys) { // not for every recording again
		logx.Printf("hotkeys: watching %d pause and %d stop key(s), %d app(s) with their own, overlay key %q, launchpad key %q",
			len(k.Global.Pause), len(k.Global.Stop), len(k.Apps), k.Global.Overlay.String(), k.Global.Launchpad.String())
	}
	if !slices.Equal(slices.Sorted(maps.Keys(k.Apps)), slices.Sorted(maps.Keys(a.keys.Apps))) {
		a.refreshRunning() // apps with keys of their own came or went
	}
	a.keys = k
	a.syncPTTLoan() // the watcher or its keys may have changed
}

// sameKeymap reports whether two keymaps bind alike.
func sameKeymap(x, y input.Keymap) bool {
	list := func(p, q []input.Combo) bool { return slices.EqualFunc(p, q, input.Combo.Equal) }
	same := func(a, b input.Bindings) bool {
		return list(a.Pause, b.Pause) && list(a.Stop, b.Stop) && a.Overlay.Equal(b.Overlay) && a.Launchpad.Equal(b.Launchpad)
	}
	return same(x.Global, y.Global) && maps.EqualFunc(x.Apps, y.Apps, same)
}

func (a *App) closeHotkeys() {
	a.endLaunchpad() // no release will be reported
	if a.hotkeys != nil {
		a.reclaimPTT()
		a.hotkeys.Close()
		a.hotkeys = nil
		logx.Printf("hotkeys: off")
	}
	a.ptt.userTalk, a.ptt.pauseKey = false, false // no release will be reported
	a.keys = input.Keymap{}
	a.fgApp = "" // no app is followed without the watcher; keybindsChanged syncs held keys
}

// onHotkey runs on the UI thread.
func (a *App) onHotkey(ev input.HotkeyEvent) {
	if a.onOverlayEvent(ev) || a.onLaunchpadEvent(ev) {
		return
	}
	switch ev.Action {
	case input.ActionHandOver:
		if !ev.Down {
			logx.Printf("hotkey: letting go of push-to-talk before the key failed")
		}
		a.syncPTTLoan() // the hook let go of the keys: the flow stops showing them held
		a.render()
		return
	case input.ActionApp:
		a.setForegroundApp(ev.App)
		return
	}
	if ev.Down && a.ptt.capturing {
		return // the user is recording a key in the panel
	}
	switch ev.Action {
	case input.ActionTalk:
		a.onTalk(ev.Down, ev.Index)
	case input.ActionStop:
		logx.Printf("hotkey: stop")
		a.reclaimPTT() // keys the hook let go of for the stop key aren't shown held
		a.stopAll()
	case input.ActionPause:
		a.onPauseKey(ev.Down)
	}
	a.render()
}

// setForegroundApp follows the app in the foreground: its push-to-talk
// keys apply from now on, also to keys held right now. A new app may have
// just started, so which apps run is checked again too.
func (a *App) setForegroundApp(app string) {
	if app == a.fgApp {
		return
	}
	a.fgApp = app
	if a.appKeybinds(app) != nil {
		logx.Printf("keybinds: %s in the foreground", app)
	}
	a.syncPTTKeys()
	a.closeOverlayWithoutKey()
	a.refreshRunning()
}

// refreshRunning finds out, off the UI thread, which programs run, for the
// home page's note on apps with keybinds of their own.
func (a *App) refreshRunning() {
	if len(a.cfg.AppKeybinds) == 0 {
		a.running = nil
		return
	}
	go func() {
		running := input.RunningApps()
		a.tray.Post(func() {
			before := a.runningKeysStatus()
			a.running = running
			if a.runningKeysStatus() != before && a.tray.Popup().Visible() {
				a.render()
			}
		})
	}()
}
