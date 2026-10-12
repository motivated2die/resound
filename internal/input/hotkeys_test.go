package input

import (
	"reflect"
	"testing"
)

const (
	vkLCtrl = 0xA2
	vkRCtrl = 0xA3
	vkF     = 0x46
	vkG     = 0x47
	vkX     = 0x58
)

func TestMatcherPause(t *testing.T) {
	b := &Bindings{Pause: []Combo{{vkLCtrl, vkF}, {vkG}}}
	var m Matcher
	steps := []struct {
		vk   uint16
		down bool
		want []HotkeyEvent
	}{
		{vkF, true, nil}, // F alone is not Ctrl+F
		{vkRCtrl, true, []HotkeyEvent{{ActionPause, true}}}, // right Ctrl counts as Ctrl
		{vkF, true, nil},  // auto-repeat
		{vkG, true, nil},  // a second pause binding while already paused
		{vkF, false, nil}, // G still holds the pause
		{vkG, false, []HotkeyEvent{{ActionPause, false}}},
		{vkRCtrl, false, nil},
	}
	for i, s := range steps {
		if got := m.Key(b, s.vk, s.down); !reflect.DeepEqual(got, s.want) {
			t.Fatalf("step %d (vk %#x down=%v): got %v, want %v", i, s.vk, s.down, got, s.want)
		}
	}
}

func TestMatcherStopOncePerPress(t *testing.T) {
	b := &Bindings{Stop: []Combo{{vkX}}}
	var m Matcher
	stop := []HotkeyEvent{{ActionStop, true}}
	if got := m.Key(b, vkX, true); !reflect.DeepEqual(got, stop) {
		t.Fatalf("press = %v", got)
	}
	if got := m.Key(b, vkX, true); got != nil {
		t.Fatalf("auto-repeat = %v, want nothing", got)
	}
	if got := m.Key(b, vkX, false); got != nil {
		t.Fatalf("release = %v, want nothing", got)
	}
	if got := m.Key(b, vkX, true); !reflect.DeepEqual(got, stop) {
		t.Fatalf("second press = %v", got)
	}
}

func TestMatcherRebind(t *testing.T) {
	var m Matcher
	b := &Bindings{Pause: []Combo{{vkG}}}
	m.Key(b, vkG, true)
	if got := m.Eval(&Bindings{}); !reflect.DeepEqual(got, []HotkeyEvent{{ActionPause, false}}) {
		t.Fatalf("clearing the bindings while held = %v, want a pause release", got)
	}
	if got := m.Eval(nil); got != nil {
		t.Fatalf("nil bindings = %v", got)
	}
}

func TestSameCombo(t *testing.T) {
	if !SameCombo(Combo{vkLCtrl, vkF}, Combo{vkF, vkRCtrl}) {
		t.Error("Left Ctrl+F and F+Right Ctrl should be the same hotkey")
	}
	if SameCombo(Combo{vkLCtrl, vkF}, Combo{vkF}) || SameCombo(Combo{vkF}, Combo{vkG}) {
		t.Error("different combos compared equal")
	}
}
