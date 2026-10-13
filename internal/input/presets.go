package input

// Preset is a ready-made push-to-talk binding.
type Preset struct {
	Keys Combo
	Note string // why it is a good choice
}

// Presets are keys games accept as bindings but almost no keyboard has, so
// pressing them from Resound never collides with anything the user types.
// F13–F24 have real scan codes and are recognised by DirectInput and raw
// input, unlike most "virtual only" keys.
var Presets = func() []Preset {
	var p []Preset
	for vk := uint16(0x7C); vk <= 0x87; vk++ { // F13 … F24
		p = append(p, Preset{Keys: Combo{vk}, Note: "unused F-keys"})
	}
	p = append(p,
		Preset{Keys: Combo{0x13}, Note: "rarely used"}, // Pause/Break
		Preset{Keys: Combo{0x91}, Note: "rarely used"}, // Scroll Lock
		Preset{Keys: Combo{VKXButton1}, Note: "mouse"},
		Preset{Keys: Combo{VKXButton2}, Note: "mouse"},
		Preset{Keys: Combo{VKMiddle}, Note: "mouse"},
	)
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
