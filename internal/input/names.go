package input

import "fmt"

// fixedName names keys that Windows' GetKeyNameText gets wrong or leaves
// blank: F13–F24 have scan codes no common layout labels, and Pause shares
// its scan code (0x45) with Num Lock. Only the display changes; the keys are
// still sent by their own virtual-key and scan codes.
func fixedName(vk uint16) (string, bool) {
	switch {
	case vk >= 0x70 && vk <= 0x87: // VK_F1 … VK_F24
		return fmt.Sprintf("F%d", vk-0x6F), true
	case vk == VKMiddle:
		return "Mouse 3", true
	case vk == VKXButton1:
		return "Mouse 4", true
	case vk == VKXButton2:
		return "Mouse 5", true
	case vk == 0x13:
		return "Pause", true
	case vk == 0x91:
		return "Scroll Lock", true
	}
	return "", false
}
