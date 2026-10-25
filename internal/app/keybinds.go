package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/logx"
)

// Keybinds: what keys and mouse buttons do, everywhere and per app. Four
// functions can be bound: push-to-talk (Resound presses its keys while
// sounds play), pause (held: the sounds pause, push-to-talk and the mic are
// let go), stop (pressed: all sounds stop) and the overlay's key (opens the
// in-game overlay, overlay.go). The global keys apply everywhere. An app
// the user added (config.AppKeybinds) can replace them function by function
// while it is in the foreground: with keys of its own, or with none, which
// turns the function off there. Functions it leaves alone keep the global
// keys. The global overlay key is set on the overlay's page, the others on
// the Keybinds page.
//
// Each function takes several combinations. Push-to-talk presses all of
// them together (a game's and Discord's, say); any one of the others
// triggers its function.

// keyFunc is a function keys can be bound to.
type keyFunc int

const (
	fnPTT keyFunc = iota
	fnPause
	fnStop
	fnOverlay
)

// appKeyFuncs are the functions an app can bind for itself, in the panel's
// order.
var appKeyFuncs = []keyFunc{fnPTT, fnPause, fnStop, fnOverlay}

func (f keyFunc) title() string { return [...]string{"Push-to-talk", "Pause", "Stop", "Overlay"}[f] }

// noun names a key of the function in a sentence: "the pause key".
func (f keyFunc) noun() string {
	return [...]string{"a push-to-talk key", "a pause key", "a stop key", "the overlay key"}[f]
}

// maxKeys is how many combinations a function takes in one scope: more
// would not fit its card, and the overlay has one key.
func (f keyFunc) maxKeys() int {
	switch f {
	case fnPTT:
		return 4
	case fnOverlay:
		return 1
	}
	return 8
}

// keyTarget is a function's keys in one scope: app "" is everywhere, else
// an app's file name (config.AppKeybinds.Exe).
type keyTarget struct {
	app string
	fn  keyFunc
}

func (t keyTarget) String() string {
	if t.app == "" {
		return strings.ToLower(t.fn.title())
	}
	return strings.ToLower(t.fn.title()) + " in " + t.app
}

// globalKeys returns a function's global combinations.
func (a *App) globalKeys(fn keyFunc) [][]uint16 {
	c := &a.cfg
	switch fn {
	case fnPTT:
		if len(c.PushToTalkKeys) == 0 {
			return nil
		}
		return append([][]uint16{c.PushToTalkKeys}, c.PushToTalkExtra...)
	case fnPause:
		return c.PauseHotkeys
	case fnStop:
		return c.StopHotkeys
	case fnOverlay:
		if len(c.OverlayKeys) > 0 {
			return [][]uint16{c.OverlayKeys}
		}
	}
	return nil
}

// setGlobalKeys replaces a function's global combinations. Push-to-talk
// keeps its first one where versions before several had their only one
// (pushToTalkKeys), so going back to such a version keeps it.
func (a *App) setGlobalKeys(fn keyFunc, keys [][]uint16) {
	c := &a.cfg
	switch fn {
	case fnPTT:
		c.PushToTalkKeys, c.PushToTalkExtra = nil, nil
		if len(keys) > 0 {
			c.PushToTalkKeys = keys[0]
		}
		if len(keys) > 1 {
			c.PushToTalkExtra = keys[1:]
		}
	case fnPause:
		c.PauseHotkeys = keys
	case fnStop:
		c.StopHotkeys = keys
	case fnOverlay:
		c.OverlayKeys = nil
		if len(keys) > 0 {
			c.OverlayKeys = keys[0]
		}
	}
}

// appKeybinds returns an app's entry, nil when it has none.
func (a *App) appKeybinds(exe string) *config.AppKeybinds {
	for i := range a.cfg.AppKeybinds {
		if a.cfg.AppKeybinds[i].Exe == exe {
			return &a.cfg.AppKeybinds[i]
		}
	}
	return nil
}

// override returns where an app's entry keeps its override of fn; nil
// without an entry.
func override(e *config.AppKeybinds, fn keyFunc) **config.KeyOverride {
	if e == nil {
		return nil
	}
	switch fn {
	case fnPTT:
		return &e.PushToTalk
	case fnPause:
		return &e.Pause
	case fnStop:
		return &e.Stop
	case fnOverlay:
		return &e.Overlay
	}
	return nil
}

// hasOverrides reports whether an app replaces any of the global keys.
func hasOverrides(e *config.AppKeybinds) bool {
	return e.PushToTalk != nil || e.Pause != nil || e.Stop != nil || e.Overlay != nil
}

// keybind is a function's keys in one scope, as they apply there: an app
// without its own keys for the function has the global ones (own false);
// one that turned it off has none (off).
type keybind struct {
	keys     [][]uint16
	own, off bool
}

func (a *App) keybind(t keyTarget) keybind {
	if t.app == "" {
		return keybind{keys: a.globalKeys(t.fn), own: true}
	}
	switch o := override(a.appKeybinds(t.app), t.fn); {
	case o == nil || *o == nil:
		return keybind{keys: a.globalKeys(t.fn)}
	case (*o).Off:
		return keybind{own: true, off: true}
	default:
		return keybind{keys: (*o).Keys, own: true}
	}
}

// keysFor returns the combinations fn has while app is in the foreground.
func (a *App) keysFor(app string, fn keyFunc) [][]uint16 { return a.keybind(keyTarget{app, fn}).keys }

// combos converts saved combinations.
func combos(keys [][]uint16) []input.Combo {
	out := make([]input.Combo, len(keys))
	for i, k := range keys {
		out[i] = input.Combo(k)
	}
	return out
}

// keysText names combinations for the panel: "Pause · Ctrl + Mouse 5".
func keysText(keys [][]uint16) string {
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = input.Combo(k).String()
	}
	return strings.Join(names, " · ")
}

// keyConflict says why c can't be added to t, "" when it can. Wherever the
// keys of t apply (t's app; for a global key also every app that keeps the
// global keys of that function), a combination does one thing only: it
// isn't twice in a list, isn't another function's key there, and is never
// a push-to-talk key Resound presses there, which would trigger itself.
func (a *App) keyConflict(t keyTarget, c input.Combo) string {
	own := a.keybind(t)
	if t.fn != fnOverlay && own.own && len(own.keys) >= t.fn.maxKeys() {
		return "That's the most keys this can hold."
	}
	scopes := []string{t.app}
	if t.app == "" {
		for i := range a.cfg.AppKeybinds {
			e := &a.cfg.AppKeybinds[i]
			if o := override(e, t.fn); o == nil || *o == nil {
				scopes = append(scopes, e.Exe)
			}
		}
	}
	for _, s := range scopes {
		for _, fn := range []keyFunc{fnPTT, fnPause, fnStop, fnOverlay} {
			if !slices.ContainsFunc(a.keysFor(s, fn), func(k []uint16) bool { return input.SameCombo(k, c) }) {
				continue
			}
			if fn == t.fn && s == t.app && !own.own {
				continue // the global keys shown in the app: taking one as its own is fine
			}
			switch {
			case fn == t.fn && fn == fnOverlay:
				return c.String() + " is the overlay key already."
			case fn == t.fn && s == t.app:
				return c.String() + " is in this list already."
			case fn == fnPTT:
				return c.String() + " is a push-to-talk key Resound presses" + a.inApp(s) + "."
			default:
				return c.String() + " is " + fn.noun() + a.inApp(s) + " already."
			}
		}
	}
	return ""
}

// inApp is " in <app's name>" for an app, "" for global keys.
func (a *App) inApp(exe string) string {
	if exe == "" {
		return ""
	}
	return " in " + a.appName(exe)
}

// appName is what the panel calls an app.
func (a *App) appName(exe string) string {
	if e := a.appKeybinds(exe); e != nil && e.Name != "" {
		return e.Name
	}
	return strings.TrimSuffix(exe, ".exe")
}

// addKeys adds a recorded or chosen combination to t, unless it can't be;
// the note on its card says why. An overlay key is replaced.
func (a *App) addKeys(t keyTarget, c input.Combo) {
	a.ptt.note, a.ptt.noteFor = "", t
	if msg := a.keyConflict(t, c); msg != "" {
		a.ptt.note = msg
		a.render()
		return
	}
	c = slices.Clone(c)
	if t.app == "" {
		keys := slices.Clone(a.globalKeys(t.fn))
		if t.fn == fnOverlay {
			keys = nil
		}
		a.setGlobalKeys(t.fn, append(keys, c))
	} else {
		o := override(a.appKeybinds(t.app), t.fn)
		if o == nil {
			return
		}
		if *o == nil || (*o).Off || t.fn == fnOverlay {
			*o = &config.KeyOverride{}
		}
		(*o).Keys = append((*o).Keys, c)
	}
	logx.Printf("keybinds: %s added to %s", c, t)
	a.keybindsChanged()
}

// removeKeys removes t's i-th combination. An app whose last own key goes
// keeps the global keys again.
func (a *App) removeKeys(t keyTarget, i int) {
	a.ptt.note = ""
	if t.app == "" {
		keys := a.globalKeys(t.fn)
		if i < 0 || i >= len(keys) {
			return
		}
		logx.Printf("keybinds: %s removed from %s", input.Combo(keys[i]), t)
		a.setGlobalKeys(t.fn, slices.Delete(slices.Clone(keys), i, i+1))
	} else {
		o := override(a.appKeybinds(t.app), t.fn)
		if o == nil || *o == nil || i < 0 || i >= len((*o).Keys) {
			return
		}
		logx.Printf("keybinds: %s removed from %s", input.Combo((*o).Keys[i]), t)
		(*o).Keys = slices.Delete((*o).Keys, i, i+1)
		if len((*o).Keys) == 0 {
			*o = nil
		}
	}
	a.keybindsChanged()
}

// setKeysOff turns t's function off in its app ("None").
func (a *App) setKeysOff(t keyTarget) {
	a.ptt.note = ""
	if o := override(a.appKeybinds(t.app), t.fn); o != nil {
		*o = &config.KeyOverride{Off: true}
		logx.Printf("keybinds: %s off", t)
		a.keybindsChanged()
	}
}

// resetKeys gives t's app the global keys of its function again.
func (a *App) resetKeys(t keyTarget) {
	a.ptt.note = ""
	if o := override(a.appKeybinds(t.app), t.fn); o != nil && *o != nil {
		*o = nil
		logx.Printf("keybinds: %s uses the global keys", t)
		a.keybindsChanged()
	}
}

// keybindsChanged saves the keybinds and applies them: the hooks, and the
// push-to-talk keys held right now.
func (a *App) keybindsChanged() {
	a.save()
	a.applyHotkeys()
	a.syncPTTKeys()
	a.closeOverlayWithoutKey()
	a.render()
}

// addApp gives a program keybinds of its own (none replaced yet) and opens
// its page. One already added just opens.
func (a *App) addApp(app input.App) {
	if a.appKeybinds(app.Exe) == nil {
		a.cfg.AppKeybinds = append(a.cfg.AppKeybinds, config.AppKeybinds{Exe: app.Exe, Name: app.Name})
		a.save()
		logx.Printf("keybinds: %s (%s) added", app.Exe, app.Name)
	}
	a.openAppKeys(app.Exe)
}

// removeApp forgets an app's keybinds; the global keys apply there again.
func (a *App) removeApp(exe string) {
	a.cfg.AppKeybinds = slices.DeleteFunc(a.cfg.AppKeybinds, func(e config.AppKeybinds) bool { return e.Exe == exe })
	if len(a.cfg.AppKeybinds) == 0 {
		a.cfg.AppKeybinds = nil
	}
	logx.Printf("keybinds: %s removed", exe)
	a.keysApp, a.removingApp = "", false
	a.back()
	a.keybindsChanged()
}

// overrideSummary says what an app replaces: "Push-to-talk, pause", "Uses
// the global keys".
func overrideSummary(e *config.AppKeybinds) string {
	var parts []string
	for _, fn := range appKeyFuncs {
		if o := override(e, fn); *o != nil {
			parts = append(parts, strings.ToLower(fn.title()))
		}
	}
	if len(parts) == 0 {
		return "Uses the global keys"
	}
	s := strings.Join(parts, ", ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// runningKeysStatus names the apps with keys of their own that run now, for
// the home page: "Valorant keybinds", "Keybinds for 2 apps"; "" for none.
func (a *App) runningKeysStatus() string {
	var names []string
	for i := range a.cfg.AppKeybinds {
		e := &a.cfg.AppKeybinds[i]
		if hasOverrides(e) && a.running[e.Exe] {
			names = append(names, a.appName(e.Exe))
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " keybinds"
	}
	return fmt.Sprintf("Keybinds for %d apps", len(names))
}
