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
	pressed input.Combo // keys currently held down by Resound: all of held's, each once
	held    [][]uint16  // the combinations pressed is made of (pttCombos when pressed)
	lent    bool        // pressed is lent to the hotkey watcher (lendPTT)

	capturing bool
	target    keyTarget // what the recorded key is for
	captured  input.Combo
	down      map[uint16]bool

	note    string    // why the last recorded or chosen key wasn't added
	noteFor keyTarget // the card note belongs to

	sendIn  int       // seconds until "send to game" taps the keys; 0 = idle
	sendFor keyTarget // whose keys it sends
}

func (p *pttState) init() {
	p.reasons = map[string]bool{}
	p.down = map[uint16]bool{}
}

// pttCombos are the push-to-talk combinations that apply now: those of the
// app in the foreground (its own, none if it turned push-to-talk off), else
// the global ones.
func (a *App) pttCombos() [][]uint16 { return a.keysFor(a.fgApp, fnPTT) }

// pttUnion is every key of the combinations, each once, in order: the keys
// Resound holds for them all at once.
func pttUnion(keys [][]uint16) input.Combo {
	var u input.Combo
	for _, c := range keys {
		for _, vk := range c {
			if !slices.Contains(u, vk) {
				u = append(u, vk)
			}
		}
	}
	return u
}

// setPTT adds or removes a reason and presses/releases the keys when the
// first reason appears or the last one goes away.
func (a *App) setPTT(reason string, on bool) {
	a.reclaimPTT()
	defer a.lendPTT()
	p := &a.ptt
	if on {
		p.reasons[reason] = true
	} else {
		delete(p.reasons, reason)
	}
	switch {
	case len(p.reasons) > 0 && p.pressed == nil:
		held := a.pttCombos()
		keys := pttUnion(held)
		if len(keys) == 0 {
			return
		}
		if err := input.Press(keys); err != nil {
			logx.Printf("push-to-talk press: %v", err)
		}
		p.pressed, p.held = keys, held
		logx.Printf("push-to-talk down (%s)", keysText(held))
	case len(p.reasons) == 0 && p.pressed != nil:
		a.releaseAllPTT()
	}
}

// syncPTTKeys switches held push-to-talk keys to the ones that apply now,
// after another app came to the foreground or the keys changed: it lets go
// of the keys no longer wanted and presses the new ones. Keys in both stay
// down, so a key both apps use never bounces.
func (a *App) syncPTTKeys() {
	p := &a.ptt
	if len(p.reasons) == 0 {
		return
	}
	a.reclaimPTT()
	defer a.lendPTT()
	held := a.pttCombos()
	keys := pttUnion(held)
	if p.pressed.Equal(keys) {
		p.held = held
		return
	}
	var up, down input.Combo
	for _, vk := range p.pressed {
		if !slices.Contains(keys, vk) {
			up = append(up, vk)
		}
	}
	for _, vk := range keys {
		if !slices.Contains(p.pressed, vk) {
			down = append(down, vk)
		}
	}
	if err := input.Release(up); err != nil {
		logx.Printf("push-to-talk release: %v", err)
	}
	if err := input.Press(down); err != nil {
		logx.Printf("push-to-talk press: %v", err)
	}
	if len(keys) == 0 {
		keys, held = nil, nil
	}
	logx.Printf("push-to-talk now %q (was %q)", keysText(held), keysText(p.held))
	p.pressed, p.held = keys, held
}

// lendPTT lends the held keys to the hotkey watcher while only playback
// holds them: a pause or stop key then lets go of them in the hook, before
// the game sees that key (input.Watcher.LendPTT). Talk-through keeps them,
// as the user talks on Resound's channel then.
func (a *App) lendPTT() {
	p := &a.ptt
	if p.lent || p.pressed == nil || a.hotkeys == nil || p.capturing || a.cfg.NoPushToTalkHandOver ||
		len(p.reasons) != 1 || !p.reasons[pttPlayback] || !a.keys.Hotkeys() {
		return
	}
	a.hotkeys.LendPTT(slices.Clone(p.pressed))
	p.lent = true
}

// reclaimPTT ends the loan. If a pause or stop key used the keys
// meanwhile, the hook let go of them already.
func (a *App) reclaimPTT() {
	p := &a.ptt
	if !p.lent {
		return
	}
	p.lent = false
	if a.hotkeys == nil || a.hotkeys.TakePTT() != nil {
		return // still held
	}
	logx.Printf("push-to-talk up (%s) before a hotkey", keysText(p.held))
	p.pressed, p.held = nil, nil
}

// syncPTTLoan lends or takes back the keys after a setting changed.
func (a *App) syncPTTLoan() {
	a.reclaimPTT()
	a.lendPTT()
}

// releaseAllPTT lets go of the keys, whatever the reasons say.
func (a *App) releaseAllPTT() {
	a.reclaimPTT()
	p := &a.ptt
	if p.pressed == nil {
		return
	}
	if err := input.Release(p.pressed); err != nil {
		logx.Printf("push-to-talk release: %v", err)
	}
	logx.Printf("push-to-talk up (%s)", keysText(p.held))
	p.pressed, p.held = nil, nil
}

// startCapture records the next key or combination pressed over the panel
// for t.
func (a *App) startCapture(t keyTarget) {
	p := &a.ptt
	p.capturing, p.target, p.note = true, t, ""
	p.captured = nil
	clear(p.down)
	a.reclaimPTT() // the keys recorded aren't held back
	if a.hotkeys != nil {
		a.hotkeys.SetKeymap(a.keymap()) // without the overlay key
	}
	a.render()
}

// cancelCapture stops recording without a key.
func (a *App) cancelCapture() {
	if !a.ptt.capturing {
		return
	}
	a.ptt.capturing = false
	a.applyHotkeys() // the overlay key works again
	a.render()
}

func (a *App) togglePTTHandOver() {
	a.cfg.NoPushToTalkHandOver = !a.cfg.NoPushToTalkHandOver
	a.syncPTTLoan()
	a.save()
	logx.Printf("push-to-talk hand-over: %v", !a.cfg.NoPushToTalkHandOver)
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

// onPanelKey records a combination: every key or mouse button (3–5, over
// the panel) pressed until the first one is released. Esc cancels.
func (a *App) onPanelKey(ev ui.KeyEvent) bool {
	p := &a.ptt
	if !p.capturing {
		return false
	}
	if ev.VK == 0x1B { // Esc
		if !ev.Down {
			a.cancelCapture()
		}
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
		a.applyHotkeys() // the overlay key works again
		a.addKeys(p.target, slices.Clone(p.captured))
	}
	return true
}

// keyPresets are the ready-made keys offered while recording; an app's
// list starts with None, which turns the function off there.
func keyPresets(t keyTarget) []ui.Option {
	var opts []ui.Option
	if t.app != "" {
		opts = append(opts, ui.Option{Label: "None", Detail: "off in this app"})
	}
	for _, p := range input.Presets {
		opts = append(opts, ui.Option{Label: p.Keys.String(), Detail: p.Note})
	}
	return opts
}

// choosePreset takes the i-th of keyPresets(t) while recording for t.
func (a *App) choosePreset(t keyTarget, i int) {
	a.ptt.capturing = false
	a.applyHotkeys()
	if t.app != "" {
		if i == 0 {
			a.setKeysOff(t)
			return
		}
		i--
	}
	if i >= 0 && i < len(input.Presets) {
		a.addKeys(t, slices.Clone(input.Presets[i].Keys))
		return
	}
	a.render()
}

// pttSendDelay is the countdown in seconds before "send it to the game".
const pttSendDelay = 5

// sendPTTToGame taps t's push-to-talk keys after a countdown, so the user
// can click the game's keybind field first. This is how keys that don't
// exist on the keyboard (F13–F24) or are awkward to press get bound in the
// game.
func (a *App) sendPTTToGame(t keyTarget) {
	keys := pttUnion(a.keysFor(t.app, fnPTT))
	if a.ptt.sendIn > 0 || len(keys) == 0 {
		return
	}
	a.ptt.sendIn, a.ptt.sendFor = pttSendDelay, t
	a.render()
	go func() {
		for range pttSendDelay {
			time.Sleep(time.Second)
			a.tray.Post(func() {
				a.ptt.sendIn--
				if a.ptt.sendIn == 0 {
					a.tapPTT(keys)
				}
				a.render()
			})
		}
	}()
}

// tapPTT presses and releases keys once (about 150 ms), unless push-to-talk
// is held for playback or talk-through.
func (a *App) tapPTT(keys input.Combo) {
	if a.ptt.pressed != nil {
		return
	}
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
