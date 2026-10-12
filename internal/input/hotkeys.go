package input

// Hotkeys: keys the user holds or presses anywhere in Windows to pause or
// stop Resound's sounds (see Watcher). The matching is portable and tested;
// watching the keyboard is Windows-only (hook_windows.go).

// Action is what a hotkey does.
type Action int

const (
	ActionPause Action = iota // held: pause playback; released: resume
	ActionStop                // pressed: stop all sounds
)

func (a Action) String() string {
	if a == ActionStop {
		return "stop"
	}
	return "pause"
}

// HotkeyEvent is a hotkey becoming active (Down) or inactive. Stop keys
// only report Down.
type HotkeyEvent struct {
	Action Action
	Down   bool
}

// Bindings are the hotkey combinations; any one of them triggers its action.
type Bindings struct {
	Pause, Stop []Combo
}

// Empty reports whether no hotkey is set.
func (b Bindings) Empty() bool { return len(b.Pause) == 0 && len(b.Stop) == 0 }

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
