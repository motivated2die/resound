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
		{vkRCtrl, true, []HotkeyEvent{{ActionPause, true, 0}}}, // right Ctrl counts as Ctrl
		{vkF, true, nil},  // auto-repeat
		{vkG, true, nil},  // a second pause binding while already paused
		{vkF, false, nil}, // G still holds the pause
		{vkG, false, []HotkeyEvent{{ActionPause, false, 0}}},
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
	stop := []HotkeyEvent{{ActionStop, true, 0}}
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
	if got := m.Eval(&Bindings{}); !reflect.DeepEqual(got, []HotkeyEvent{{ActionPause, false, 0}}) {
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

// The overlay key opens and selects and is always swallowed, with its
// repeats and its release; Esc and the wheel are swallowed only while the
// overlay is open; other keys always pass.
func TestFilterOverlay(t *testing.T) {
	const vkF8, vkW = 0x77, 0x57
	b := &Bindings{Overlay: Combo{vkF8}, Pause: []Combo{{vkG}}}
	var f Filter
	type ev = HotkeyEvent
	steps := []struct {
		name    string
		vk      uint16
		down    bool
		want    []HotkeyEvent
		swallow bool
	}{
		{"Esc while closed passes", vkEscape, true, nil, false},
		{"its release too", vkEscape, false, nil, false},
		{"overlay key opens", vkF8, true, []ev{{ActionOverlayOpen, true, 0}}, true},
		{"auto-repeat swallowed", vkF8, true, nil, true},
		{"release swallowed", vkF8, false, nil, true},
		{"other keys pass while open", vkW, true, nil, false},
		{"and are released", vkW, false, nil, false},
		{"pause keys still work", vkG, true, []ev{{ActionPause, true, 0}}, false},
		{"and release", vkG, false, []ev{{ActionPause, false, 0}}, false},
		{"overlay key selects", vkF8, true, []ev{{ActionOverlaySelect, true, 0}}, true},
		{"release swallowed", vkF8, false, nil, true},
		{"Esc after select passes", vkEscape, true, nil, false},
		{"release", vkEscape, false, nil, false},
		{"open again", vkF8, true, []ev{{ActionOverlayOpen, true, 0}}, true},
		{"Esc down while the key is held", vkEscape, true, []ev{{ActionOverlayEscDown, true, 0}}, true},
		{"Esc repeat swallowed", vkEscape, true, nil, true},
		{"overlay key release swallowed", vkF8, false, nil, true},
		{"tapping Esc closes on its release", vkEscape, false, []ev{{ActionOverlayClose, true, 0}}, true},
		{"Esc while closed passes again", vkEscape, true, nil, false},
		{"release", vkEscape, false, nil, false},
	}
	for i, s := range steps {
		got, swallow := f.Key(b, s.vk, s.down)
		if !reflect.DeepEqual(got, s.want) || swallow != s.swallow {
			t.Fatalf("step %d %s: got %v swallow=%v, want %v swallow=%v", i, s.name, got, swallow, s.want, s.swallow)
		}
	}
	if f.Open() {
		t.Fatal("still open after Esc")
	}
}

// Of a combination, only the key completing it is swallowed.
func TestFilterOverlayCombo(t *testing.T) {
	b := &Bindings{Overlay: Combo{vkLCtrl, vkF}}
	var f Filter
	if _, sw := f.Key(b, vkLCtrl, true); sw {
		t.Fatal("Ctrl swallowed")
	}
	if ev, sw := f.Key(b, vkF, true); !sw || len(ev) != 1 || ev[0].Action != ActionOverlayOpen {
		t.Fatalf("Ctrl+F: %v %v", ev, sw)
	}
	if _, sw := f.Key(b, vkLCtrl, false); sw {
		t.Fatal("Ctrl release swallowed")
	}
	if _, sw := f.Key(b, vkF, false); !sw {
		t.Fatal("F release passed")
	}
	// F alone doesn't select.
	if ev, sw := f.Key(b, vkF, true); sw || len(ev) != 0 {
		t.Fatalf("F alone: %v %v", ev, sw)
	}
}

// The wheel moves a step per notch, collects precise wheels' small deltas,
// and only while the overlay is open.
func TestFilterWheel(t *testing.T) {
	var f Filter
	if ev, sw := f.Wheel(120); ev != nil || sw {
		t.Fatal("wheel taken while closed")
	}
	f.SetOpen(true)
	if ev, sw := f.Wheel(-240); !sw || len(ev) != 1 || ev[0].Steps != 2 {
		t.Fatalf("two notches down: %v", ev)
	}
	if ev, sw := f.Wheel(60); !sw || ev != nil {
		t.Fatalf("half a notch: %v", ev)
	}
	if ev, _ := f.Wheel(60); len(ev) != 1 || ev[0].Steps != -1 {
		t.Fatalf("second half up: %v", ev)
	}
	f.SetOpen(false)
	if _, sw := f.Wheel(120); sw {
		t.Fatal("wheel taken after closing")
	}
}

// Holding Esc in the open overlay until EscTimeout stops the sounds; its
// release is swallowed afterwards and doesn't close anything again. A
// timeout after an early release does nothing.
func TestFilterEscHold(t *testing.T) {
	const vkF8 = 0x77
	b := &Bindings{Overlay: Combo{vkF8}}
	var f Filter
	f.Key(b, vkF8, true)
	f.Key(b, vkF8, false)
	if ev, sw := f.Key(b, vkEscape, true); !sw || len(ev) != 1 || ev[0].Action != ActionOverlayEscDown || !f.EscHeld() {
		t.Fatalf("Esc down: %v %v", ev, sw)
	}
	if ev := f.EscTimeout(); len(ev) != 1 || ev[0].Action != ActionOverlayStop || f.Open() {
		t.Fatalf("held: %v open=%v", ev, f.Open())
	}
	if ev, sw := f.Key(b, vkEscape, false); !sw || len(ev) != 0 {
		t.Fatalf("release after the stop: %v %v", ev, sw)
	}
	// A tap, then the timer of that press firing late: nothing.
	f.Key(b, vkF8, true)
	f.Key(b, vkF8, false)
	f.Key(b, vkEscape, true)
	f.Key(b, vkEscape, false)
	if ev := f.EscTimeout(); ev != nil {
		t.Fatalf("timeout after a tap: %v", ev)
	}
}
