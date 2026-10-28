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
		{vkRCtrl, true, []HotkeyEvent{{Action: ActionPause, Down: true}}}, // right Ctrl counts as Ctrl
		{vkF, true, nil},  // auto-repeat
		{vkG, true, nil},  // a second pause binding while already paused
		{vkF, false, nil}, // G still holds the pause
		{vkG, false, []HotkeyEvent{{Action: ActionPause, Down: false}}},
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
	stop := []HotkeyEvent{{Action: ActionStop, Down: true}}
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

// A pause lasts while its keys are held, also when the bindings change
// meanwhile (another app comes to the foreground, or the settings change):
// it never ends early or bounces, and it ends with its keys' release.
func TestMatcherPauseLatched(t *testing.T) {
	var m Matcher
	game := &Bindings{Pause: []Combo{{vkG}}, Stop: []Combo{{vkX}}}
	other := &Bindings{Stop: []Combo{{vkG}}} // G is a stop key there
	pause := []HotkeyEvent{{Action: ActionPause, Down: true}}
	if got := m.Key(game, vkG, true); !reflect.DeepEqual(got, pause) {
		t.Fatalf("G in the game = %v", got)
	}
	if got := m.Key(other, vkG, true); got != nil {
		t.Fatalf("G repeating in the other app = %v, want nothing", got)
	}
	if got := m.Key(other, vkF, true); got != nil || !m.Paused() {
		t.Fatalf("another key in the other app = %v, paused %v: the pause must last", got, m.Paused())
	}
	if got := m.Key(nil, vkF, false); got != nil {
		t.Fatalf("F up = %v", got)
	}
	if got := m.Key(other, vkG, false); !reflect.DeepEqual(got, []HotkeyEvent{{Action: ActionPause}}) {
		t.Fatalf("letting go of G = %v, want the pause to end", got)
	}
	if got := m.Key(other, vkG, true); !reflect.DeepEqual(got, []HotkeyEvent{{Action: ActionStop, Down: true}}) {
		t.Fatalf("G pressed again in the other app = %v, want its stop", got)
	}
}

// Keys held when the bindings change don't start anything: only a press
// that completes a combination does.
func TestMatcherOnlyPressesStart(t *testing.T) {
	var m Matcher
	none := &Bindings{}
	b := &Bindings{Pause: []Combo{{vkLCtrl, vkF}}, Stop: []Combo{{vkX}}}
	m.Key(none, vkLCtrl, true)
	m.Key(none, vkF, true)
	m.Key(none, vkX, true)
	if got := m.Key(b, vkG, true); got != nil {
		t.Fatalf("an unrelated press with Ctrl+F and X held = %v, want nothing", got)
	}
	if got := m.Key(b, vkF, true); got != nil {
		t.Fatalf("F repeating = %v, want nothing", got)
	}
	m.Key(b, vkF, false)
	if got := m.Key(b, vkF, true); !reflect.DeepEqual(got, []HotkeyEvent{{Action: ActionPause, Down: true}}) {
		t.Fatalf("F pressed again = %v, want the pause", got)
	}
	if got := m.Key(nil, vkLCtrl, false); !reflect.DeepEqual(got, []HotkeyEvent{{Action: ActionPause}}) {
		t.Fatalf("letting go of Ctrl = %v, want the pause to end", got)
	}
}

// Mouse buttons count like keys, alone or with keys.
func TestMatcherMouse(t *testing.T) {
	var m Matcher
	b := &Bindings{Pause: []Combo{{VKXButton1}}, Stop: []Combo{{vkLCtrl, VKMiddle}}}
	if got := m.Key(b, VKXButton1, true); !reflect.DeepEqual(got, []HotkeyEvent{{Action: ActionPause, Down: true}}) {
		t.Fatalf("Mouse 4 = %v", got)
	}
	if got := m.Key(b, VKXButton1, false); !reflect.DeepEqual(got, []HotkeyEvent{{Action: ActionPause}}) {
		t.Fatalf("Mouse 4 up = %v", got)
	}
	if got := m.Key(b, VKMiddle, true); got != nil {
		t.Fatalf("Mouse 3 alone = %v", got)
	}
	m.Key(b, VKMiddle, false)
	m.Key(b, vkRCtrl, true)
	if got := m.Key(b, VKMiddle, true); !reflect.DeepEqual(got, []HotkeyEvent{{Action: ActionStop, Down: true}}) {
		t.Fatalf("Ctrl + Mouse 3 = %v", got)
	}
	if !b.UsesMouse() || (&Bindings{Pause: []Combo{{vkG}}, Overlay: Combo{vkF}}).UsesMouse() {
		t.Error("UsesMouse is wrong")
	}
	if !(&Bindings{Overlay: Combo{vkLCtrl, VKXButton2}}).UsesMouse() {
		t.Error("an overlay key with a mouse button uses the mouse")
	}
}

func TestKeymapFor(t *testing.T) {
	k := &Keymap{
		Global: Bindings{Pause: []Combo{{vkG}}},
		Apps:   map[string]Bindings{"game.exe": {Stop: []Combo{{vkX}}}},
	}
	if b := k.For("game.exe"); len(b.Pause) != 0 || len(b.Stop) != 1 {
		t.Errorf("game.exe got %+v", b)
	}
	if b := k.For("other.exe"); len(b.Pause) != 1 {
		t.Errorf("other.exe got %+v, want the global bindings", b)
	}
	if b := k.For(""); len(b.Pause) != 1 {
		t.Errorf("an unknown app got %+v, want the global bindings", b)
	}
	if !k.Hotkeys() || (Keymap{Apps: map[string]Bindings{"a.exe": {}}}).Hotkeys() {
		t.Error("Hotkeys is wrong")
	}
	if (Keymap{Apps: map[string]Bindings{"a.exe": {}}}).Empty() || !(Keymap{}).Empty() {
		t.Error("Empty is wrong: an app of its own needs the watcher")
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
		{"overlay key opens", vkF8, true, []ev{{Action: ActionOverlayOpen, Down: true}}, true},
		{"auto-repeat swallowed", vkF8, true, nil, true},
		{"release swallowed", vkF8, false, nil, true},
		{"other keys pass while open", vkW, true, nil, false},
		{"and are released", vkW, false, nil, false},
		{"pause keys still work", vkG, true, []ev{{Action: ActionPause, Down: true}}, false},
		{"and release", vkG, false, []ev{{Action: ActionPause, Down: false}}, false},
		{"overlay key selects", vkF8, true, []ev{{Action: ActionOverlaySelect, Down: true}}, true},
		{"release swallowed", vkF8, false, nil, true},
		{"Esc after select passes", vkEscape, true, nil, false},
		{"release", vkEscape, false, nil, false},
		{"open again", vkF8, true, []ev{{Action: ActionOverlayOpen, Down: true}}, true},
		{"Esc down while the key is held", vkEscape, true, []ev{{Action: ActionOverlayEscDown, Down: true}}, true},
		{"Esc repeat swallowed", vkEscape, true, nil, true},
		{"overlay key release swallowed", vkF8, false, nil, true},
		{"tapping Esc closes on its release", vkEscape, false, []ev{{Action: ActionOverlayClose, Down: true}}, true},
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

// While the launchpad key is held, every new press is swallowed and becomes
// a launchpad event; keys held before pass, releases with their presses.
func TestFilterLaunchpad(t *testing.T) {
	const vkCaps, vkW, vk3, vkNum7, vkF1 = 0x14, 0x57, 0x33, 0x67, 0x70
	b := &Bindings{Launchpad: Combo{vkCaps}, Pause: []Combo{{vkG}}, Stop: []Combo{{vkX}}}
	var f Filter
	type ev = HotkeyEvent
	steps := []struct {
		name    string
		vk      uint16
		down    bool
		want    []HotkeyEvent
		swallow bool
	}{
		{"W held before", vkW, true, nil, false},
		{"launchpad key starts it", vkCaps, true, []ev{{Action: ActionLaunchpad, Down: true}}, true},
		{"its repeat is swallowed", vkCaps, true, nil, true},
		{"W repeating passes", vkW, true, nil, false},
		{"a digit plays", vk3, true, []ev{{Action: ActionLaunchpadKey, Down: true, Key: '3'}}, true},
		{"its repeat does nothing", vk3, true, nil, true},
		{"its release is swallowed", vk3, false, nil, true},
		{"pressed again plays again", vk3, true, []ev{{Action: ActionLaunchpadKey, Down: true, Key: '3'}}, true},
		{"release", vk3, false, nil, true},
		{"a numpad digit is that digit", vkNum7, true, []ev{{Action: ActionLaunchpadKey, Down: true, Key: '7'}}, true},
		{"release", vkNum7, false, nil, true},
		{"a letter plays", vkF, true, []ev{{Action: ActionLaunchpadKey, Down: true, Key: 'F'}}, true},
		{"release", vkF, false, nil, true},
		{"the pause key is a letter here", vkG, true, []ev{{Action: ActionLaunchpadKey, Down: true, Key: 'G'}}, true},
		{"and pauses nothing", vkG, false, nil, true},
		{"so is the stop key", vkX, true, []ev{{Action: ActionLaunchpadKey, Down: true, Key: 'X'}}, true},
		{"release", vkX, false, nil, true},
		{"Space goes down", vkSpace, true, []ev{{Action: ActionLaunchpadSpace, Down: true}}, true},
		{"its repeats are swallowed", vkSpace, true, nil, true},
		{"and up", vkSpace, false, []ev{{Action: ActionLaunchpadSpace}}, true},
		{"Left: the tab before", vkLeft, true, []ev{{Action: ActionLaunchpadTab, Down: true, Steps: -1}}, true},
		{"release", vkLeft, false, nil, true},
		{"Down: the next tab", vkDown, true, []ev{{Action: ActionLaunchpadTab, Down: true, Steps: 1}}, true},
		{"release", vkDown, false, nil, true},
		{"other keys are swallowed too", vkF1, true, nil, true},
		{"W let go reaches the game", vkW, false, nil, false},
		{"letting go ends it", vkCaps, false, []ev{{Action: ActionLaunchpad}}, true},
		{"F1 let go after it ended stays swallowed", vkF1, false, nil, true},
		{"keys pass again", vk3, true, nil, false},
		{"release", vk3, false, nil, false},
		{"the pause key pauses again", vkG, true, []ev{{Action: ActionPause, Down: true}}, false},
		{"and resumes", vkG, false, []ev{{Action: ActionPause}}, false},
	}
	for i, s := range steps {
		got, swallow := f.Key(b, s.vk, s.down)
		if !reflect.DeepEqual(got, s.want) || swallow != s.swallow {
			t.Fatalf("step %d %s: got %v swallow=%v, want %v swallow=%v", i, s.name, got, swallow, s.want, s.swallow)
		}
	}
}

// A combination holds the launchpad while all its keys are held: letting go
// of the key that passed (Ctrl) ends it as well.
func TestFilterLaunchpadCombo(t *testing.T) {
	b := &Bindings{Launchpad: Combo{vkLCtrl, vkF}}
	var f Filter
	if _, sw := f.Key(b, vkLCtrl, true); sw {
		t.Fatal("Ctrl swallowed")
	}
	if ev, sw := f.Key(b, vkF, true); !sw || !reflect.DeepEqual(ev, []HotkeyEvent{{Action: ActionLaunchpad, Down: true}}) {
		t.Fatalf("Ctrl+F: %v %v", ev, sw)
	}
	if ev, sw := f.Key(b, vkLCtrl, false); sw || !reflect.DeepEqual(ev, []HotkeyEvent{{Action: ActionLaunchpad}}) {
		t.Fatalf("Ctrl let go: %v %v, want the launchpad to end and Ctrl to pass", ev, sw)
	}
	if ev, sw := f.Key(b, vkF, false); !sw || ev != nil {
		t.Fatalf("F let go: %v %v, want it swallowed like its press", ev, sw)
	}
}

// The launchpad lasts while its keys are held, also when another app with
// other bindings comes to the front meanwhile.
func TestFilterLaunchpadAppSwitch(t *testing.T) {
	const vkCaps = 0x14
	game := &Bindings{Launchpad: Combo{vkCaps}}
	other := &Bindings{}
	var f Filter
	f.Key(game, vkCaps, true)
	if ev, sw := f.Key(other, vkF, true); !sw || len(ev) != 1 || ev[0].Key != 'F' {
		t.Fatalf("F in the other app: %v %v", ev, sw)
	}
	f.Key(other, vkF, false)
	if ev, _ := f.Key(other, vkCaps, false); !reflect.DeepEqual(ev, []HotkeyEvent{{Action: ActionLaunchpad}}) {
		t.Fatalf("let go in the other app: %v", ev)
	}
}

// Starting the launchpad closes the overlay; the wheel then switches tabs;
// ending it at once (EndLaunchpad) lets keys pass again.
func TestFilterLaunchpadOverlayAndWheel(t *testing.T) {
	const vkCaps, vkF8 = 0x14, 0x77
	b := &Bindings{Launchpad: Combo{vkCaps}, Overlay: Combo{vkF8}}
	var f Filter
	f.Key(b, vkF8, true)
	f.Key(b, vkF8, false)
	if !f.Open() {
		t.Fatal("overlay didn't open")
	}
	want := []HotkeyEvent{{Action: ActionOverlayClose, Down: true}, {Action: ActionLaunchpad, Down: true}}
	if ev, _ := f.Key(b, vkCaps, true); !reflect.DeepEqual(ev, want) || f.Open() {
		t.Fatalf("launchpad over the open overlay: %v, open %v", ev, f.Open())
	}
	if ev, sw := f.Wheel(-240); !sw || !reflect.DeepEqual(ev, []HotkeyEvent{{Action: ActionLaunchpadTab, Down: true, Steps: 2}}) {
		t.Fatalf("two notches towards the user: %v", ev)
	}
	if ev, sw := f.Key(b, vkF8, true); !sw || ev != nil {
		t.Fatalf("overlay key while held: %v %v, want it swallowed and nothing else", ev, sw)
	}
	f.Key(b, vkF8, false)
	if ev := f.EndLaunchpad(); !reflect.DeepEqual(ev, []HotkeyEvent{{Action: ActionLaunchpad}}) || f.Launchpad() {
		t.Fatalf("EndLaunchpad: %v", ev)
	}
	if _, sw := f.Wheel(120); sw {
		t.Fatal("wheel taken after the launchpad ended")
	}
	if _, sw := f.Key(b, vkCaps, false); !sw {
		t.Fatal("the key's late release passed: its press was swallowed")
	}
	if ev, sw := f.Key(b, vkF, true); sw || ev != nil {
		t.Fatalf("F after the end: %v %v", ev, sw)
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

func TestHandsOver(t *testing.T) {
	b := &Bindings{Pause: []Combo{{vkLCtrl, vkF}}, Stop: []Combo{{vkX}}}
	var f Filter
	steps := []struct {
		vk         uint16
		down, want bool
	}{
		{vkLCtrl, true, false}, // not a hotkey yet
		{vkF, true, true},      // completes the pause
		{vkF, true, false},     // auto-repeat
		{vkX, true, true},      // a stop while paused
		{vkX, false, false},
		{vkF, false, false}, // the pause ends
		{vkLCtrl, false, false},
	}
	for i, s := range steps {
		events, swallow := f.Key(b, s.vk, s.down)
		if swallow {
			t.Fatalf("step %d: the filter swallowed a hotkey", i)
		}
		if got := handsOver(events); got != s.want {
			t.Fatalf("step %d (vk %#x down=%v): handsOver %v, want %v (events %v)", i, s.vk, s.down, got, s.want, events)
		}
	}
}

// The user's own push-to-talk presses: a talk starts on the press that
// completes a talk combination (reporting which one), lasts while one is
// held, and works alongside pause keys. Repeats cause nothing.
func TestMatcherTalk(t *testing.T) {
	const vkPause, vkM4 = 0x13, VKXButton1
	b := &Bindings{Talk: []Combo{{vkPause}, {vkLCtrl, vkM4}}, Pause: []Combo{{vkG}}}
	var m Matcher
	type ev = HotkeyEvent
	steps := []struct {
		vk   uint16
		down bool
		want []HotkeyEvent
	}{
		{vkLCtrl, true, nil},
		{vkM4, true, []ev{{Action: ActionTalk, Down: true, Index: 1}}}, // Ctrl + Mouse 4
		{vkM4, true, nil},    // repeat
		{vkPause, true, nil}, // a second talk combination while talking
		{vkM4, false, nil},   // Pause still holds the talk
		{vkG, true, []ev{{Action: ActionPause, Down: true}}},
		{vkPause, false, []ev{{Action: ActionTalk}}},
		{vkG, false, []ev{{Action: ActionPause}}},
		{vkLCtrl, false, nil},
		{vkPause, true, []ev{{Action: ActionTalk, Down: true, Index: 0}}},
		{vkPause, false, []ev{{Action: ActionTalk}}},
	}
	for i, s := range steps {
		if got := m.Key(b, s.vk, s.down); !reflect.DeepEqual(got, s.want) {
			t.Fatalf("step %d (vk %#x down=%v): got %v, want %v", i, s.vk, s.down, got, s.want)
		}
		if m.Talking() != (i >= 1 && i <= 5 || i == 9) {
			t.Fatalf("step %d: talking %v", i, m.Talking())
		}
	}
	if handsOver([]HotkeyEvent{{Action: ActionTalk, Down: true}}) {
		t.Error("a talk must not hand push-to-talk over")
	}
}

// A talk started in one app lasts across a switch to an app without
// push-to-talk, until its key is let go; a talk key held when the switch
// makes it one doesn't start a talk.
func TestMatcherTalkAppSwitch(t *testing.T) {
	game := &Bindings{Talk: []Combo{{vkF}}}
	other := &Bindings{}
	var m Matcher
	if got := m.Key(game, vkF, true); len(got) != 1 || got[0].Action != ActionTalk || !got[0].Down {
		t.Fatalf("press = %v", got)
	}
	if got := m.Key(other, vkG, true); got != nil || !m.Talking() {
		t.Fatalf("after the switch: %v talking=%v", got, m.Talking())
	}
	if got := m.Key(other, vkF, false); len(got) != 1 || got[0].Down {
		t.Fatalf("release = %v", got)
	}
	m.Key(other, vkG, false)
	m.Key(other, vkF, true)
	if got := m.Key(game, vkG, true); got != nil || m.Talking() {
		t.Fatalf("held before the switch: %v", got)
	}
}
