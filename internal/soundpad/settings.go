package soundpad

import "fmt"

// Soundpad keeps its settings in the registry, under HKEY_CURRENT_USER\
// Software\Leppsoft\Soundpad (readable without admin rights; its "Export
// preferences" writes the same as a .reg file). Keys are a Windows
// virtual-key code ("stopKey", 0 = none) and a modifier field
// ("stopKeyModifiers"), whose bits for Ctrl, Alt, Shift and Win aren't
// documented. So a key is taken only without modifiers, where it is just its
// virtual-key code; one with modifiers is named as left out.
//
// Its push-to-talk is "Auto keys" (MainFrame\AutoKeys): numbered entries
// "key0", "keyModifiers0", "enabled0", "type0"… Type 0 is the default,
// "press and hold" while the sound plays, which is what Resound does; the
// other type presses the key before and after the sound. An entry without a
// key has 0xFFFFFFFF.

// Settings are Soundpad's keys Resound has a use for. A nil field has none.
type Settings struct {
	// PushToTalk are the enabled auto keys that hold a key while sounds
	// play (Resound takes up to 4).
	PushToTalk [][]uint16
	Stop       []uint16
	// Pause is Soundpad's pause/resume key. Soundpad toggles with it; in
	// Resound sounds pause while it is held.
	Pause []uint16
	// Skipped says why keys that are set weren't taken.
	Skipped []string
	// Found reports whether Soundpad's settings were there at all.
	Found bool
}

// Empty reports whether no key carries over.
func (s *Settings) Empty() bool { return len(s.PushToTalk) == 0 && s.Stop == nil && s.Pause == nil }

const maxAutoKeys = 16

// parseSettings reads the keys through main (values of MainFrame) and auto
// (values of MainFrame\AutoKeys); each reports false for a missing value.
func parseSettings(main, auto func(name string) (uint32, bool)) Settings {
	s := Settings{Found: true}
	hotkey := func(name, what string) []uint16 {
		vk, _ := main(name + "Key")
		mods, _ := main(name + "KeyModifiers")
		return s.key(vk, mods, what)
	}
	s.Stop = hotkey("stop", "stop key")
	s.Pause = hotkey("pauseResume", "pause key")
	for i := range maxAutoKeys {
		vk, ok := auto(fmt.Sprintf("key%d", i))
		if !ok {
			break
		}
		if on, _ := auto(fmt.Sprintf("enabled%d", i)); on == 0 || vk == 0 || vk == 0xFFFFFFFF {
			continue
		}
		if typ, _ := auto(fmt.Sprintf("type%d", i)); typ != 0 {
			s.Skipped = append(s.Skipped, "An auto key that presses before and after a sound: Resound holds push-to-talk while sounds play.")
			continue
		}
		mods, _ := auto(fmt.Sprintf("keyModifiers%d", i))
		if k := s.key(vk, mods, "auto key"); k != nil && len(s.PushToTalk) < 4 {
			s.PushToTalk = append(s.PushToTalk, k)
		}
	}
	return s
}

// key is a key Resound can take, nil (with a note in Skipped for a set key
// that can't be).
func (s *Settings) key(vk, mods uint32, what string) []uint16 {
	switch {
	case vk == 0 || vk == 0xFFFFFFFF:
		return nil
	case vk > 254:
		s.Skipped = append(s.Skipped, "The "+what+" isn't a key Resound knows.")
		return nil
	case vk == 0x01 || vk == 0x02:
		s.Skipped = append(s.Skipped, "The "+what+" is the left or right mouse button, which Resound can't use.")
		return nil
	case mods != 0:
		s.Skipped = append(s.Skipped, "The "+what+" uses Ctrl, Alt, Shift or Win, which can't be read from Soundpad's settings reliably. Set it in the steps.")
		return nil
	}
	return []uint16{uint16(vk)}
}
