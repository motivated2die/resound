package app

import (
	"strings"
	"testing"

	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/input"
)

const (
	vkPause, vkScroll, vkG, vkV, vkX = 0x13, 0x91, 0x47, 0x56, 0x58
	vkMouse4                         = input.VKXButton1
)

// keybindsApp has global keys (push-to-talk Pause, pause G, stop X) and two
// games: one with its own push-to-talk (Mouse 4) and pause turned off, one
// added without keys of its own.
func keybindsApp() *App {
	return &App{cfg: config.Config{
		PushToTalkKeys: []uint16{vkPause},
		PauseHotkeys:   [][]uint16{{vkG}},
		StopHotkeys:    [][]uint16{{vkX}},
		AppKeybinds: []config.AppKeybinds{
			{Exe: "game.exe", Name: "Game",
				PushToTalk: &config.KeyOverride{Keys: [][]uint16{{vkMouse4}}},
				Pause:      &config.KeyOverride{Off: true}},
			{Exe: "plain.exe", Name: "Plain"},
		},
	}}
}

func TestKeybindScopes(t *testing.T) {
	a := keybindsApp()
	for _, c := range []struct {
		t        keyTarget
		keys     string
		own, off bool
	}{
		{keyTarget{"", fnPTT}, "Pause", true, false},
		{keyTarget{"game.exe", fnPTT}, "Mouse 4", true, false},
		{keyTarget{"game.exe", fnPause}, "", true, true},
		{keyTarget{"game.exe", fnStop}, "X", false, false},   // keeps the global stop key
		{keyTarget{"plain.exe", fnPause}, "G", false, false}, // keeps everything
		{keyTarget{"other.exe", fnPTT}, "Pause", false, false},
	} {
		kb := a.keybind(c.t)
		if got := keysText(kb.keys); got != c.keys || kb.own != c.own || kb.off != c.off {
			t.Errorf("%v: keys %q own %v off %v, want %q %v %v", c.t, got, kb.own, kb.off, c.keys, c.own, c.off)
		}
	}
	a.fgApp = "game.exe"
	if got := keysText(a.pttCombos()); got != "Mouse 4" {
		t.Errorf("push-to-talk in the game: %q", got)
	}
	a.fgApp = "plain.exe"
	if got := keysText(a.pttCombos()); got != "Pause" {
		t.Errorf("push-to-talk in the plain app: %q", got)
	}
}

// The watcher gets complete bindings for the apps that replace something,
// the global ones merged in; apps without keys of their own need nothing.
func TestKeymap(t *testing.T) {
	a := keybindsApp()
	a.cfg.OverlayKeys = []uint16{0x77}
	k := a.keymap()
	if len(k.Global.Pause) != 1 || len(k.Global.Stop) != 1 || len(k.Global.Overlay) != 1 {
		t.Errorf("global: %+v", k.Global)
	}
	game, ok := k.Apps["game.exe"]
	if !ok || len(k.Apps) != 1 {
		t.Fatalf("apps: %v", k.Apps)
	}
	if len(game.Pause) != 0 || len(game.Stop) != 1 || len(game.Overlay) != 1 {
		t.Errorf("game: %+v, want no pause, the global stop and overlay keys", game)
	}
	a.ptt.capturing = true
	if k := a.keymap(); len(k.Global.Overlay) != 0 || len(k.Apps["game.exe"].Overlay) != 0 {
		t.Error("the overlay key should be free while recording")
	}
}

func TestKeyConflict(t *testing.T) {
	a := keybindsApp()
	for _, c := range []struct {
		t    keyTarget
		keys input.Combo
		want string // part of the reason, "" for allowed
	}{
		{keyTarget{"", fnPause}, input.Combo{vkG}, "in this list already"},
		{keyTarget{"", fnPause}, input.Combo{vkX}, "a stop key already"},
		{keyTarget{"", fnStop}, input.Combo{vkPause}, "push-to-talk key Resound presses"},
		// Global keys apply in apps that keep them: Mouse 4 is the game's
		// push-to-talk, and the game keeps the global stop keys.
		{keyTarget{"", fnStop}, input.Combo{vkMouse4}, "push-to-talk key Resound presses in Game"},
		// The game has its own push-to-talk: a global one doesn't reach it.
		{keyTarget{"", fnPTT}, input.Combo{vkV}, ""},
		// The game turned pause off, so a global pause key doesn't apply there.
		{keyTarget{"", fnPause}, input.Combo{vkMouse4}, ""},
		{keyTarget{"game.exe", fnPause}, input.Combo{vkMouse4}, "push-to-talk key Resound presses in Game"},
		{keyTarget{"game.exe", fnPause}, input.Combo{vkX}, "a stop key in Game already"},
		{keyTarget{"game.exe", fnPause}, input.Combo{vkV}, ""},
		// A key the app shows from the global list can become its own.
		{keyTarget{"plain.exe", fnPause}, input.Combo{vkG}, ""},
		{keyTarget{"plain.exe", fnPTT}, input.Combo{vkG}, "a pause key in Plain already"},
		{keyTarget{"", fnOverlay}, input.Combo{vkX}, "a stop key already"},
		{keyTarget{"", fnOverlay}, input.Combo{vkMouse4}, "push-to-talk key Resound presses in Game"},
		{keyTarget{"", fnOverlay}, input.Combo{vkScroll}, ""},
	} {
		got := a.keyConflict(c.t, c.keys)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s to %v: %q, want %q", c.keys, c.t, got, c.want)
		}
	}
	a.cfg.PushToTalkExtra = [][]uint16{{1}, {2}, {3}}
	if got := a.keyConflict(keyTarget{"", fnPTT}, input.Combo{vkV}); !strings.Contains(got, "most") {
		t.Errorf("a fifth push-to-talk key: %q", got)
	}
}

func TestGlobalPTTKeys(t *testing.T) {
	a := &App{}
	a.setGlobalKeys(fnPTT, [][]uint16{{vkPause}, {vkScroll}})
	if len(a.cfg.PushToTalkKeys) != 1 || len(a.cfg.PushToTalkExtra) != 1 {
		t.Errorf("two: %v %v", a.cfg.PushToTalkKeys, a.cfg.PushToTalkExtra)
	}
	if got := keysText(a.globalKeys(fnPTT)); got != "Pause · Scroll Lock" {
		t.Errorf("globalKeys = %q", got)
	}
	a.setGlobalKeys(fnPTT, [][]uint16{{vkScroll}})
	if a.cfg.PushToTalkKeys[0] != vkScroll || a.cfg.PushToTalkExtra != nil {
		t.Errorf("one: %v %v", a.cfg.PushToTalkKeys, a.cfg.PushToTalkExtra)
	}
	if got := pttUnion([][]uint16{{0xA2, vkG}, {0xA2, vkV}, {vkPause}}); !got.Equal(input.Combo{0xA2, vkG, vkV, vkPause}) {
		t.Errorf("union %v: shared keys are held once", got)
	}
}

func TestRunningKeysStatus(t *testing.T) {
	a := keybindsApp()
	if s := a.runningKeysStatus(); s != "" {
		t.Errorf("nothing running: %q", s)
	}
	a.running = map[string]bool{"plain.exe": true}
	if s := a.runningKeysStatus(); s != "" {
		t.Errorf("only an app without keys of its own: %q", s)
	}
	a.running["game.exe"] = true
	if s := a.runningKeysStatus(); s != "Game keybinds" {
		t.Errorf("the game: %q", s)
	}
	a.cfg.AppKeybinds[1].Stop = &config.KeyOverride{Keys: [][]uint16{{vkV}}}
	if s := a.runningKeysStatus(); s != "Keybinds for 2 apps" {
		t.Errorf("both: %q", s)
	}
	if s := overrideSummary(&a.cfg.AppKeybinds[0]); s != "Push-to-talk, pause" {
		t.Errorf("summary %q", s)
	}
}
