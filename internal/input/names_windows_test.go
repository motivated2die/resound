package input

import (
	"strings"
	"testing"
)

func TestPresetNames(t *testing.T) {
	want := map[uint16]string{0x7C: "F13", 0x80: "F17", 0x87: "F24", 0x13: "Pause", 0x91: "Scroll Lock", VKXButton1: "Mouse 4"}
	for vk, name := range want {
		if got := Name(vk); got != name {
			t.Errorf("Name(%#x) = %q, want %q", vk, got, name)
		}
	}
	for _, p := range Presets {
		if s := p.Keys.String(); s == "" || strings.HasPrefix(s, "Key 0x") {
			t.Errorf("preset %v has no readable name: %q", p.Keys, s)
		}
	}
	// Other keys keep the keyboard language's names ("Left Ctrl", "Left Strg").
	if got := (Combo{0xA2, 0x7C}).String(); !strings.HasPrefix(got, "Left ") || !strings.HasSuffix(got, " + F13") {
		t.Errorf("combo = %q", got)
	}
}
