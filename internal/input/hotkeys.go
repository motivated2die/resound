package input

import (
	"slices"
	"time"
)

// Hotkeys: keys and mouse buttons the user holds or presses anywhere in
// Windows to pause (while held, or until pressed again) or stop Resound's
// sounds, to open the in-game overlay, or
// to hold the launchpad (see Watcher). Each app can have bindings of its own (Keymap). The
// matching is portable and tested; watching the keyboard and mouse is
// Windows-only (hook_windows.go).

// Action is what a hotkey does.
type Action int

const (
	ActionPause       Action = iota // held: pause playback; released: resume
	ActionStop                      // pressed: stop all sounds
	ActionTogglePause               // pressed: pause playback, or resume it

	// The overlay's events (Filter): its key pressed while it is closed
	// opens it, pressed again plays the highlighted sound, and the wheel
	// moves the highlight by Steps (positive: down the list). Esc going
	// down while it is open reports EscDown; let go before EscHoldTime it
	// closes the overlay (Close), held that long it closes it and stops
	// all sounds (Stop).
	ActionOverlayOpen
	ActionOverlaySelect
	ActionOverlayClose
	ActionOverlayScroll
	ActionOverlayEscDown
	ActionOverlayStop

	// ActionHandOver reports that a pause or stop key let go of the
	// push-to-talk keys lent to the hook (Watcher.LendPTT) before passing
	// the key on. Down is false if SendInput failed.
	ActionHandOver

	// ActionApp reports that another app came to the foreground: App is its
	// file name in lower case ("" when unknown), whose bindings apply now
	// (Keymap). Resound's own windows never count. Down is always true.
	ActionApp

	// ActionTalk reports the user pressing (Down) and letting go of one of
	// the push-to-talk keys Resound presses (Bindings.Talk) themselves. Index
	// is the combination the press completed. Like a pause, it lasts while a
	// talk combination is held. Its keys always pass to the game.
	ActionTalk

	// The launchpad's events (Filter): its key going down starts it
	// (ActionLaunchpad, Down), letting go ends it. While it is held, every
	// new press of a key or of the middle or side buttons is kept from the
	// other programs: a digit or letter reports ActionLaunchpadKey with Key
	// ('0'–'9', numpad digits as those, 'A'–'Z'), Space ActionLaunchpadSpace
	// (Down, and Up when it is let go, even after the launchpad ended: a
	// long press can mean something else), the arrow keys and the mouse
	// wheel ActionLaunchpadTab by Steps (positive: the next tab). Other keys
	// do nothing.
	ActionLaunchpad
	ActionLaunchpadKey
	ActionLaunchpadSpace
	ActionLaunchpadTab
)

// EscHoldTime is how long Esc is held in the open overlay to stop all
// sounds.
const EscHoldTime = 700 * time.Millisecond

func (a Action) String() string {
	return [...]string{"pause", "stop", "toggle pause", "overlay open", "overlay select", "overlay close", "overlay scroll", "overlay Esc down", "overlay stop", "push-to-talk hand-over", "app", "talk",
		"launchpad", "launchpad key", "launchpad Space", "launchpad tab"}[a]
}

// HotkeyEvent is a hotkey becoming active (Down) or inactive. Stop and
// toggle pause keys and
// the overlay's events only report Down, as do the launchpad's but its own
// and Space's.
type HotkeyEvent struct {
	Action Action
	Down   bool
	Steps  int    // ActionOverlayScroll and ActionLaunchpadTab only
	App    string // ActionApp only
	Index  int    // ActionTalk only
	Key    uint16 // ActionLaunchpadKey only: '0'–'9' or 'A'–'Z'
}

// Bindings are the hotkey combinations; any one of them triggers its action.
// A combination may hold mouse buttons 3–5 as well as keys.
type Bindings struct {
	Pause, Stop []Combo
	// TogglePause keys pause or resume when pressed. Unlike pause and stop
	// keys they never take part in the push-to-talk hand-over (handsOver):
	// they aren't a game's push-to-talk, so the app lets go of push-to-talk
	// itself once the press arrives.
	TogglePause []Combo
	Overlay     Combo // opens the in-game overlay; empty = no overlay
	Launchpad   Combo // held: the launchpad; empty = none
	// Talk are the push-to-talk combinations Resound presses, while the
	// user's own presses of them count (ActionTalk); none otherwise.
	Talk []Combo
}

// Empty reports whether no hotkey is set.
func (b Bindings) Empty() bool {
	return len(b.Pause) == 0 && len(b.Stop) == 0 && len(b.TogglePause) == 0 && len(b.Overlay) == 0 && len(b.Talk) == 0 && len(b.Launchpad) == 0
}

// UsesMouse reports whether a combination holds a mouse button, which the
// mouse hook must watch.
func (b Bindings) UsesMouse() bool {
	for _, list := range [][]Combo{b.Pause, b.Stop, b.TogglePause, {b.Overlay, b.Launchpad}, b.Talk} {
		for _, c := range list {
			if slices.ContainsFunc(c, IsMouse) {
				return true
			}
		}
	}
	return false
}

// Keymap is the bindings of every app: Global, unless the app in the
// foreground has bindings of its own in Apps (by its file name in lower
// case, e.g. "valorant.exe"), complete and already merged with the global
// ones where it keeps those.
type Keymap struct {
	Global Bindings
	Apps   map[string]Bindings
}

// For returns the bindings that apply while app is in the foreground.
func (k *Keymap) For(app string) *Bindings {
	if k == nil {
		return nil
	}
	if b, ok := k.Apps[app]; ok {
		return &b
	}
	return &k.Global
}

// Empty reports whether no app has a hotkey and no app has bindings of its
// own (whose foreground the app may follow, ActionApp).
func (k Keymap) Empty() bool { return k.Global.Empty() && len(k.Apps) == 0 }

// Hotkeys reports whether a pause or stop key is set for any app: a key
// that lets go of lent push-to-talk keys (Watcher.LendPTT). Toggle pause
// keys don't.
func (k Keymap) Hotkeys() bool {
	if len(k.Global.Pause) > 0 || len(k.Global.Stop) > 0 {
		return true
	}
	for _, b := range k.Apps {
		if len(b.Pause) > 0 || len(b.Stop) > 0 {
			return true
		}
	}
	return false
}

// sameKey folds the left/right variants of Shift, Ctrl, Alt and Windows into
// one key, so "Left Ctrl + F" also matches Right Ctrl + F.
func sameKey(vk uint16) uint16 {
	switch vk {
	case 0xA0, 0xA1:
		return 0x10 // VK_SHIFT
	case 0xA2, 0xA3:
		return 0x11 // VK_CONTROL
	case 0xA4, 0xA5:
		return 0x12 // VK_MENU
	case 0x5C:
		return 0x5B // VK_LWIN
	}
	return vk
}

// IsMouse reports whether vk is a mouse button. Only the middle button and
// the side buttons (VKMiddle, VKXButton1, VKXButton2) can be bound: the
// left and right ones work the panel and aim in games.
func IsMouse(vk uint16) bool { return vk >= 0x01 && vk <= 0x06 && vk != 0x03 }

// Matcher turns key and mouse button presses and releases into hotkey
// events. It is not safe for concurrent use; the Watcher drives it from its
// hook thread.
//
// An action starts on the press that completes one of its combinations: a
// combination that becomes complete in another way, because the bindings
// changed while its keys were held (another app came to the foreground),
// does nothing until it is pressed again. A pause lasts while any pause
// combination is held, of the bindings that started it as well as of the
// current ones, so an app switch while a pause key is held never ends the
// pause early or lets it bounce.
// The user's push-to-talk (talk) lasts the same way.
type Matcher struct {
	down  map[uint16]bool // physical keys held now (left/right kept apart)
	pause latch
	talk  latch
}

// latch is an action that lasts while one of its combinations is held: on
// since a press completed one, with the combinations in effect then (by).
type latch struct {
	on bool
	by []Combo
}

// step updates l after a press (press) or release of vk with the current
// combinations, and reports what changed: start (with the index of the
// combination completed) or end.
func (m *Matcher) step(l *latch, combos []Combo, vk uint16, press bool) (start, end bool, index int) {
	switch {
	case l.on && !m.anyHeld(combos) && !m.anyHeld(l.by):
		l.on, l.by = false, nil
		return false, true, 0
	case !l.on && press:
		if i := m.completes(combos, vk); i >= 0 {
			l.on, l.by = true, combos
			return true, false, i
		}
	}
	return false, false, 0
}

// Key records a press or release and returns the hotkey events it causes.
// Auto-repeated presses cause none.
func (m *Matcher) Key(b *Bindings, vk uint16, down bool) []HotkeyEvent {
	if m.down == nil {
		m.down = map[uint16]bool{}
	}
	press := down && !m.down[vk]
	if down {
		m.down[vk] = true
	} else {
		delete(m.down, vk)
	}
	var pause, stop, toggle, talk []Combo
	if b != nil {
		pause, stop, toggle, talk = b.Pause, b.Stop, b.TogglePause, b.Talk
	}
	var events []HotkeyEvent
	if start, end, _ := m.step(&m.pause, pause, vk, press); start || end {
		events = append(events, HotkeyEvent{Action: ActionPause, Down: start})
	}
	if start, end, i := m.step(&m.talk, talk, vk, press); start || end {
		events = append(events, HotkeyEvent{Action: ActionTalk, Down: start, Index: i})
	}
	if press && m.completes(stop, vk) >= 0 {
		events = append(events, HotkeyEvent{Action: ActionStop, Down: true})
	}
	if press && m.completes(toggle, vk) >= 0 {
		events = append(events, HotkeyEvent{Action: ActionTogglePause, Down: true})
	}
	return events
}

// Paused reports whether a pause key holds the pause.
func (m *Matcher) Paused() bool { return m.pause.on }

// Talking reports whether the user holds a push-to-talk key themselves.
func (m *Matcher) Talking() bool { return m.talk.on }

// Held returns the keys the matcher thinks are down.
func (m *Matcher) Held() []uint16 {
	keys := make([]uint16, 0, len(m.down))
	for vk := range m.down {
		keys = append(keys, vk)
	}
	return keys
}

// completes returns the index of the combination pressing vk completed: one
// that holds vk and is held now; -1 for none.
func (m *Matcher) completes(combos []Combo, vk uint16) int {
	for i, c := range combos {
		if m.held(c) && slices.ContainsFunc(c, func(k uint16) bool { return sameKey(k) == sameKey(vk) }) {
			return i
		}
	}
	return -1
}

func (m *Matcher) anyHeld(combos []Combo) bool {
	for _, c := range combos {
		if m.held(c) {
			return true
		}
	}
	return false
}

// held reports whether every key of c is down. An empty combo never is.
func (m *Matcher) held(c Combo) bool {
	if len(c) == 0 {
		return false
	}
	for _, want := range c {
		if !m.isDown(want) {
			return false
		}
	}
	return true
}

// isDown reports whether want, or its other left/right variant, is down.
func (m *Matcher) isDown(want uint16) bool {
	for vk := range m.down {
		if sameKey(vk) == sameKey(want) {
			return true
		}
	}
	return false
}

// completesWith reports whether pressing vk would complete c: c holds vk,
// and its other keys are down already.
func (m *Matcher) completesWith(c Combo, vk uint16) bool {
	if !slices.ContainsFunc(c, func(k uint16) bool { return sameKey(k) == sameKey(vk) }) {
		return false
	}
	for _, want := range c {
		if sameKey(want) != sameKey(vk) && !m.isDown(want) {
			return false
		}
	}
	return true
}

// hold records vk as down without matching anything: a press the launchpad
// took, which starts no pause, stop or talk.
func (m *Matcher) hold(vk uint16) {
	if m.down == nil {
		m.down = map[uint16]bool{}
	}
	m.down[vk] = true
}

// handsOver reports whether events start a pause or a stop: the moment the
// push-to-talk keys Resound holds must go up, before the key that caused it
// reaches the game (Watcher.LendPTT). A toggle pause key doesn't: it
// passes at once, never held back, and the app lets go of push-to-talk.
func handsOver(events []HotkeyEvent) bool {
	for _, ev := range events {
		if ev.Down && (ev.Action == ActionPause || ev.Action == ActionStop) {
			return true
		}
	}
	return false
}

// SameCombo reports whether two combinations trigger the same way: the same
// keys in any order, left and right modifiers alike.
func SameCombo(a, b Combo) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if sameKey(x) == sameKey(y) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// The overlay takes a few inputs away from the game while it needs them,
// and nothing else: its own key (always, as the user dedicated it to the
// overlay), and Esc and the mouse wheel only while it is open. A key whose
// press was swallowed has its auto-repeats and its release swallowed too,
// so the game never sees half a key press. Of a combination, only the key
// that completes it is swallowed; the others have reached the game already.
//
// Filter decides this inside the hook callback, which must answer at once,
// so it also keeps whether the overlay is open: a quick Esc right after
// opening is caught even before the UI thread has caught up.
//
// The launchpad takes more, but only while its key is held: every new press
// (keys, the middle and side buttons) and the wheel, so nothing typed into
// it reaches the game. Keys held before it started still pass, their
// releases too, so the game never keeps a key it saw go down. It starts no
// pause, stop or talk meanwhile; one that started before ends as usual. Its
// start closes the overlay.

const (
	vkEscape   = 0x1B
	vkSpace    = 0x20
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkNumpad0  = 0x60
	wheelDelta = 120 // one notch (WHEEL_DELTA); precise wheels send less per event
)

// Filter adds the overlay and the launchpad to a Matcher. It is not safe for
// concurrent use; the Watcher drives it from its hook thread.
type Filter struct {
	Matcher
	open        bool
	escHeld     bool // Esc went down while open and isn't up yet
	overlayHeld bool
	swallowed   map[uint16]bool // keys whose press was swallowed
	wheel       int             // wheel delta short of a whole step
	launchpad   Combo           // the combination holding the launchpad; nil = not held
	space       bool            // Space went down while the launchpad was held and isn't up yet
}

// Key records a press or release. It returns the hotkey events it causes
// and whether the key must be kept from the other programs.
func (f *Filter) Key(b *Bindings, vk uint16, down bool) (events []HotkeyEvent, swallow bool) {
	if f.swallowed == nil {
		f.swallowed = map[uint16]bool{}
	}
	if f.swallowed[vk] {
		if down {
			return nil, true // auto-repeat
		}
		delete(f.swallowed, vk)
		if vk == vkSpace && f.space {
			f.space = false
			events = append(events, HotkeyEvent{Action: ActionLaunchpadSpace})
		}
		if vk == vkEscape && f.escHeld { // let go before EscHoldTime
			f.SetOpen(false)
			return []HotkeyEvent{{Action: ActionOverlayClose, Down: true}}, true
		}
		events = append(events, f.Matcher.Key(b, vk, false)...)
		f.overlayHeld = b != nil && f.held(b.Overlay)
		return append(events, f.launchpadLetGo()...), true
	}
	press := down && !f.down[vk]
	switch {
	case f.launchpad != nil && press:
		f.hold(vk)
		f.swallowed[vk] = true
		f.space = f.space || vk == vkSpace
		return launchpadInput(vk), true
	case press && b != nil && f.completesWith(b.Launchpad, vk):
		f.hold(vk)
		f.swallowed[vk] = true
		f.launchpad, f.wheel = b.Launchpad, 0
		if f.open {
			f.SetOpen(false)
			events = []HotkeyEvent{{Action: ActionOverlayClose, Down: true}}
		}
		return append(events, HotkeyEvent{Action: ActionLaunchpad, Down: true}), true
	}
	if down && vk == vkEscape && f.open {
		f.swallowed[vk] = true
		f.escHeld = true // EscTimeout or the release decides
		return []HotkeyEvent{{Action: ActionOverlayEscDown, Down: true}}, true
	}
	events = f.Matcher.Key(b, vk, down)
	held := b != nil && f.held(b.Overlay)
	if held && !f.overlayHeld && down {
		f.overlayHeld = true
		f.swallowed[vk] = true
		action := ActionOverlayOpen
		if f.open {
			action = ActionOverlaySelect
		}
		f.SetOpen(!f.open)
		return append(events, HotkeyEvent{Action: action, Down: true}), true
	}
	f.overlayHeld = held
	if !down {
		events = append(events, f.launchpadLetGo()...) // a key of its combination may pass, e.g. Ctrl
	}
	return events, false
}

// launchpadInput is what a press does while the launchpad is held.
func launchpadInput(vk uint16) []HotkeyEvent {
	switch {
	case vk >= '0' && vk <= '9', vk >= 'A' && vk <= 'Z':
		return []HotkeyEvent{{Action: ActionLaunchpadKey, Down: true, Key: vk}}
	case vk >= vkNumpad0 && vk <= vkNumpad0+9:
		return []HotkeyEvent{{Action: ActionLaunchpadKey, Down: true, Key: '0' + vk - vkNumpad0}}
	case vk == vkSpace:
		return []HotkeyEvent{{Action: ActionLaunchpadSpace, Down: true}}
	case vk == vkLeft || vk == vkUp:
		return []HotkeyEvent{{Action: ActionLaunchpadTab, Down: true, Steps: -1}}
	case vk == vkRight || vk == vkDown:
		return []HotkeyEvent{{Action: ActionLaunchpadTab, Down: true, Steps: 1}}
	}
	return nil
}

// launchpadLetGo ends the launchpad once its combination isn't held any
// more, whatever the bindings are now (another app may have come to the
// front meanwhile).
func (f *Filter) launchpadLetGo() []HotkeyEvent {
	if f.launchpad == nil || f.held(f.launchpad) {
		return nil
	}
	return f.EndLaunchpad()
}

// EndLaunchpad ends the launchpad at once, e.g. when its key's release
// can't arrive (the secure desktop). Keys whose press it swallowed keep
// having their releases swallowed.
func (f *Filter) EndLaunchpad() []HotkeyEvent {
	if f.launchpad == nil {
		return nil
	}
	f.launchpad, f.wheel = nil, 0
	return []HotkeyEvent{{Action: ActionLaunchpad}}
}

// Launchpad reports whether the launchpad is held.
func (f *Filter) Launchpad() bool { return f.launchpad != nil }

// Swallowed reports whether vk's press was swallowed and its release hasn't
// come yet.
func (f *Filter) Swallowed(vk uint16) bool { return f.swallowed[vk] }

// Wheel records a mouse wheel turn (delta as in WM_MOUSEWHEEL: positive
// away from the user). While the overlay is open it is swallowed and moves
// the highlight a step per notch; while the launchpad is held it moves a
// tab per notch (towards the user: the next one).
func (f *Filter) Wheel(delta int) (events []HotkeyEvent, swallow bool) {
	action := ActionOverlayScroll
	switch {
	case f.launchpad != nil:
		action = ActionLaunchpadTab
	case !f.open:
		return nil, false
	}
	f.wheel += delta
	steps := f.wheel / wheelDelta
	f.wheel -= steps * wheelDelta
	if steps == 0 {
		return nil, true
	}
	return []HotkeyEvent{{Action: action, Down: true, Steps: -steps}}, true
}

// EscTimeout is called EscHoldTime after ActionOverlayEscDown. With Esc
// still held in the open overlay, it closes it and stops the sounds; its
// release is swallowed later.
func (f *Filter) EscTimeout() []HotkeyEvent {
	if !f.escHeld || !f.open {
		return nil
	}
	f.SetOpen(false)
	return []HotkeyEvent{{Action: ActionOverlayStop, Down: true}}
}

// EscHeld reports whether Esc is held in the open overlay.
func (f *Filter) EscHeld() bool { return f.escHeld }

// SetOpen records that the overlay was opened or closed (also by the app,
// e.g. its preview button).
func (f *Filter) SetOpen(open bool) {
	f.open, f.wheel, f.escHeld = open, 0, false
}

// Open reports whether the overlay is open.
func (f *Filter) Open() bool { return f.open }
