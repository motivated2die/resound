package input

// Hotkeys: keys the user holds or presses anywhere in Windows to pause or
// stop Resound's sounds, or to open the in-game overlay (see Watcher). The
// matching is portable and tested; watching the keyboard is Windows-only
// (hook_windows.go).

// Action is what a hotkey does.
type Action int

const (
	ActionPause Action = iota // held: pause playback; released: resume
	ActionStop                // pressed: stop all sounds

	// The overlay's events (Filter): its key pressed while it is closed
	// opens it, pressed again plays the highlighted sound, Esc closes it,
	// and the wheel moves the highlight by Steps (positive: down the list).
	ActionOverlayOpen
	ActionOverlaySelect
	ActionOverlayClose
	ActionOverlayScroll
)

func (a Action) String() string {
	return [...]string{"pause", "stop", "overlay open", "overlay select", "overlay close", "overlay scroll"}[a]
}

// HotkeyEvent is a hotkey becoming active (Down) or inactive. Stop keys and
// the overlay's events only report Down.
type HotkeyEvent struct {
	Action Action
	Down   bool
	Steps  int // ActionOverlayScroll only
}

// Bindings are the hotkey combinations; any one of them triggers its action.
type Bindings struct {
	Pause, Stop []Combo
	Overlay     Combo // opens the in-game overlay; empty = no overlay
}

// Empty reports whether no hotkey is set.
func (b Bindings) Empty() bool { return len(b.Pause) == 0 && len(b.Stop) == 0 && len(b.Overlay) == 0 }

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

// IsMouse reports whether vk is a mouse button (hotkeys are keyboard-only).
func IsMouse(vk uint16) bool { return vk >= 0x01 && vk <= 0x06 && vk != 0x03 }

// Matcher turns key presses and releases into hotkey events. It is not safe
// for concurrent use; the Watcher drives it from its hook thread.
type Matcher struct {
	down     map[uint16]bool // physical keys held now (left/right kept apart)
	pauseOn  bool
	stopHeld bool
}

// Key records a press or release and returns the hotkey events it causes.
// Auto-repeated presses cause none.
func (m *Matcher) Key(b *Bindings, vk uint16, down bool) []HotkeyEvent {
	if m.down == nil {
		m.down = map[uint16]bool{}
	}
	if down {
		m.down[vk] = true
	} else {
		delete(m.down, vk)
	}
	return m.Eval(b)
}

// Held returns the keys the matcher thinks are down.
func (m *Matcher) Held() []uint16 {
	keys := make([]uint16, 0, len(m.down))
	for vk := range m.down {
		keys = append(keys, vk)
	}
	return keys
}

// Eval compares the held keys with the bindings and reports what changed,
// e.g. after the bindings were replaced.
func (m *Matcher) Eval(b *Bindings) []HotkeyEvent {
	var events []HotkeyEvent
	pause := b != nil && m.anyHeld(b.Pause)
	if pause != m.pauseOn {
		m.pauseOn = pause
		events = append(events, HotkeyEvent{Action: ActionPause, Down: pause})
	}
	stop := b != nil && m.anyHeld(b.Stop)
	if stop && !m.stopHeld {
		events = append(events, HotkeyEvent{Action: ActionStop, Down: true})
	}
	m.stopHeld = stop
	return events
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
		found := false
		for vk := range m.down {
			if sameKey(vk) == sameKey(want) {
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

const (
	vkEscape   = 0x1B
	wheelDelta = 120 // one notch (WHEEL_DELTA); precise wheels send less per event
)

// Filter adds the overlay to a Matcher. It is not safe for concurrent use;
// the Watcher drives it from its hook thread.
type Filter struct {
	Matcher
	open        bool
	overlayHeld bool
	swallowed   map[uint16]bool // keys whose press was swallowed
	wheel       int             // wheel delta short of a whole step
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
		events = f.Matcher.Key(b, vk, false)
		f.overlayHeld = b != nil && f.held(b.Overlay)
		return events, true
	}
	if down && vk == vkEscape && f.open {
		f.swallowed[vk] = true
		f.SetOpen(false)
		return []HotkeyEvent{{Action: ActionOverlayClose, Down: true}}, true
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
	return events, false
}

// Wheel records a mouse wheel turn (delta as in WM_MOUSEWHEEL: positive
// away from the user). While the overlay is open it is swallowed and moves
// the highlight a step per notch.
func (f *Filter) Wheel(delta int) (events []HotkeyEvent, swallow bool) {
	if !f.open {
		return nil, false
	}
	f.wheel += delta
	steps := f.wheel / wheelDelta
	f.wheel -= steps * wheelDelta
	if steps == 0 {
		return nil, true
	}
	return []HotkeyEvent{{Action: ActionOverlayScroll, Down: true, Steps: -steps}}, true
}

// SetOpen records that the overlay was opened or closed (also by the app,
// e.g. its preview button).
func (f *Filter) SetOpen(open bool) {
	f.open, f.wheel = open, 0
}

// Open reports whether the overlay is open.
func (f *Filter) Open() bool { return f.open }
