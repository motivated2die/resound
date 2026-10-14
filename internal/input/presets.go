package input

// Preset is a ready-made push-to-talk binding.
type Preset struct {
	Keys Combo
	Note string // why it is a good choice, or its catch
}

// Virtual keys of the numeric keypad.
const (
	VKNumpad0  = 0x60 // … VKNumpad0+9 = Numpad 9
	VKMultiply = 0x6A
	VKAdd      = 0x6B
	VKSubtract = 0x6D
	VKDivide   = 0x6F
)

// Presets are keys a game accepts as bindings but players rarely press, so
// pressing them from Resound seldom collides with anything else. Every
// engine checked (Unreal, Unity, Source, Godot) binds Pause, Scroll Lock and
// the numpad. F13–F24 never type anything, but Unreal and Source games stop
// at F12 and older Unity games at F15, so they come last.
var Presets = func() []Preset {
	p := []Preset{
		{Keys: Combo{0x13}, Note: "rarely used"}, // Pause/Break
		{Keys: Combo{0x91}, Note: "rarely used"}, // Scroll Lock
		{Keys: Combo{VKDivide}, Note: "numpad"},
		{Keys: Combo{VKMultiply}, Note: "numpad"},
		{Keys: Combo{VKSubtract}, Note: "numpad"},
		{Keys: Combo{VKAdd}, Note: "numpad"},
	}
	for vk := uint16(VKNumpad0); vk <= VKNumpad0+9; vk++ {
		p = append(p, Preset{Keys: Combo{vk}, Note: "numpad"})
	}
	p = append(p,
		Preset{Keys: Combo{VKXButton1}, Note: "mouse"},
		Preset{Keys: Combo{VKXButton2}, Note: "mouse"},
		Preset{Keys: Combo{VKMiddle}, Note: "mouse"},
	)
	for vk := uint16(0x7C); vk <= 0x87; vk++ { // F13 … F24
		p = append(p, Preset{Keys: Combo{vk}, Note: "not in all games"})
	}
	return p
}()

// Equal reports whether two combinations contain the same keys in order.
func (c Combo) Equal(o Combo) bool {
	if len(c) != len(o) {
		return false
	}
	for i := range c {
		if c[i] != o[i] {
			return false
		}
	}
	return true
}
