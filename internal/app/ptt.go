package app

import (
	"slices"
	"time"

	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// Reasons for holding the push-to-talk keys. The keys stay down while at
// least one reason is active, so talk-through and playback can overlap
// without the key ever bouncing.
const (
	pttPlayback = "playback"
	pttTalk     = "talk"
)

// pttState is UI-thread state.
type pttState struct {
	reasons map[string]bool
	pressed input.Combo // keys currently held down by Resound

	capturing bool
	target    captureTarget // what the recorded key is for
	captured  input.Combo
	down      map[uint16]bool

	hotkeyNote string        // why the last recorded hotkey wasn't added
	noteFor    captureTarget // the section hotkeyNote belongs to

	sendIn int // seconds until "send to game" taps the key; 0 = idle
}

func (p *pttState) init() {
	p.reasons = map[string]bool{}
	p.down = map[uint16]bool{}
}

// setPTT adds or removes a reason and presses/releases the keys when the
// first reason appears or the last one goes away.
func (a *App) setPTT(reason string, on bool) {
	p := &a.ptt
	if on {
		p.reasons[reason] = true
	} else {
		delete(p.reasons, reason)
	}
	switch {
	case len(p.reasons) > 0 && p.pressed == nil && len(a.cfg.PushToTalkKeys) > 0:
		keys := input.Combo(slices.Clone(a.cfg.PushToTalkKeys))
		if err := input.Press(keys); err != nil {
			logx.Printf("push-to-talk press: %v", err)
		}
		p.pressed = keys
		logx.Printf("push-to-talk down (%s)", keys)
	case len(p.reasons) == 0 && p.pressed != nil:
		a.releaseAllPTT()
	}
}

// releaseAllPTT lets go of the keys, whatever the reasons say.
func (a *App) releaseAllPTT() {
	p := &a.ptt
	if p.pressed == nil {
		return
	}
	if err := input.Release(p.pressed); err != nil {
		logx.Printf("push-to-talk release: %v", err)
	}
	logx.Printf("push-to-talk up (%s)", p.pressed)
	p.pressed = nil
}

// setPTTKeys replaces the key combination; held keys switch over at once.
func (a *App) setPTTKeys(keys input.Combo) {
	held := a.ptt.pressed != nil
	a.releaseAllPTT()
	a.cfg.PushToTalkKeys = keys
	a.save()
	if len(keys) > 0 {
		logx.Printf("push-to-talk key set to %s", keys)
	} else {
		logx.Printf("push-to-talk key cleared")
	}
	if held {
		for r := range a.ptt.reasons { // re-press with the new keys
			a.setPTT(r, true)
			break
		}
	}
	a.render()
}

func (a *App) startPTTCapture() { a.startCapture(capturePTT) }

// startCapture records the next key or combination pressed over the panel.
func (a *App) startCapture(t captureTarget) {
	p := &a.ptt
	p.capturing, p.target, p.hotkeyNote = true, t, ""
	p.captured = nil
	clear(p.down)
	if a.hotkeys != nil {
		a.hotkeys.SetBindings(a.hotkeyBindings()) // without the overlay key
	}
	a.render()
}

func (a *App) togglePTTWhilePlaying() {
	a.cfg.PushToTalkWhilePlaying = !a.cfg.PushToTalkWhilePlaying
	if !a.cfg.PushToTalkWhilePlaying {
		a.setPTT(pttPlayback, false)
	}
	a.save()
	a.render()
}

// onPanelKey records a combination: every key or mouse button pressed
// until the first one is released. Hotkeys take keyboard keys only. Esc
// cancels.
func (a *App) onPanelKey(ev ui.KeyEvent) bool {
	p := &a.ptt
	if !p.capturing {
		return false
	}
	if ev.VK == 0x1B { // Esc
		if !ev.Down {
			p.capturing = false
			a.applyHotkeys() // the overlay key works again
			a.render()
		}
		return true
	}
	if p.target != capturePTT && input.IsMouse(ev.VK) {
		return true
	}
	if ev.Down {
		if !p.down[ev.VK] {
			p.down[ev.VK] = true
			p.captured = append(p.captured, ev.VK)
			a.render()
		}
		return true
	}
	if len(p.captured) > 0 {
		p.capturing = false
		if p.target == capturePTT {
			a.setPTTKeys(slices.Clone(p.captured))
		} else {
			a.addHotkey(p.target, p.captured)
		}
		a.applyHotkeys() // the overlay key works again
	}
	return true
}

func (a *App) pttText() string {
	if len(a.cfg.PushToTalkKeys) == 0 {
		return "Not set"
	}
	return input.Combo(a.cfg.PushToTalkKeys).String()
}

// presetIndex returns the preset matching the current keys, or -1.
func (a *App) presetIndex() int {
	for i, p := range input.Presets {
		if p.Keys.Equal(a.cfg.PushToTalkKeys) {
			return i
		}
	}
	return -1
}

func (a *App) choosePreset(i int) {
	if i >= 0 && i < len(input.Presets) {
		a.setPTTKeys(slices.Clone(input.Presets[i].Keys))
	}
}

// pttSendDelay is the countdown in seconds before "send it to the game".
const pttSendDelay = 5

// sendPTTToGame taps the push-to-talk keys after a countdown, so
// the user can click the game's keybind field first. This is how keys that
// don't exist on the keyboard (F13–F24) or are awkward to press get bound
// in the game.
func (a *App) sendPTTToGame() {
	if a.ptt.sendIn > 0 || len(a.cfg.PushToTalkKeys) == 0 {
		return
	}
	a.ptt.sendIn = pttSendDelay
	a.render()
	go func() {
		for range pttSendDelay {
			time.Sleep(time.Second)
			a.tray.Post(func() {
				a.ptt.sendIn--
				if a.ptt.sendIn == 0 {
					a.tapPTT()
				}
				a.render()
			})
		}
	}()
}

// tapPTT presses and releases the keys once (about 150 ms), unless they are
// already held for playback or talk-through.
func (a *App) tapPTT() {
	if a.ptt.pressed != nil {
		return
	}
	keys := input.Combo(slices.Clone(a.cfg.PushToTalkKeys))
	if err := input.Press(keys); err != nil {
		logx.Printf("push-to-talk tap: %v", err)
		return
	}
	logx.Printf("push-to-talk sent to game (%s)", keys)
	time.AfterFunc(150*time.Millisecond, func() {
		a.tray.Post(func() {
			if a.ptt.pressed == nil { // playback may have taken over meanwhile
				_ = input.Release(keys)
			}
		})
	})
}
