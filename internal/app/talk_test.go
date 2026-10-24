package app

import (
	"strings"
	"testing"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/input"
)

// talkApp is keybindsApp holding push-to-talk with sounds as by default, its
// key presses recorded in sent ("+Pause", "-Pause") instead of sent, and the
// talk's timer queued in timers.
type talkApp struct {
	*App
	sent   []string
	timers []func()
}

func newTalkApp(t *testing.T) *talkApp {
	ta := &talkApp{App: keybindsApp()}
	ta.cfg.PushToTalkWhilePlaying = true
	ta.engine = audio.NewEngine() // no outputs: nothing plays
	ta.playback.init()
	ta.ptt.init()
	record := func(sign string) func(input.Combo) error {
		return func(c input.Combo) error {
			if len(c) > 0 {
				ta.sent = append(ta.sent, sign+c.String())
			}
			return nil
		}
	}
	oldPress, oldRelease, oldAfter := pressKeys, releaseKeys, afterUI
	pressKeys, releaseKeys = record("+"), record("-")
	afterUI = func(_ *App, _ time.Duration, fn func()) { ta.timers = append(ta.timers, fn) }
	t.Cleanup(func() { pressKeys, releaseKeys, afterUI = oldPress, oldRelease, oldAfter })
	return ta
}

// sentSince returns what was sent since the last call.
func (ta *talkApp) sentSince() string {
	s := strings.Join(ta.sent, " ")
	ta.sent = nil
	return s
}

// fire runs the queued timers.
func (ta *talkApp) fire() {
	timers := ta.timers
	ta.timers = nil
	for _, fn := range timers {
		fn()
	}
}

// The core: Resound holds push-to-talk (talk-through stands in for sounds,
// which need an output), the user presses the same key themselves. Resound
// sends no release (it would cut the user off) and presses nothing while
// they hold it; after they let go, it presses the key again.
func TestTalkTakesOverPushToTalk(t *testing.T) {
	ta := newTalkApp(t)
	ta.setPTT(pttTalk, true)
	if got := ta.sentSince(); got != "+Pause" {
		t.Fatalf("hold: %q", got)
	}
	ta.onTalk(true, 0)
	if got := ta.sentSince(); got != "" || ta.ptt.pressed != nil || !ta.ptt.userTalk {
		t.Fatalf("user's press: sent %q, pressed %v", got, ta.ptt.pressed)
	}
	ta.setPTT(pttPlayback, true)  // a sound starts meanwhile
	ta.setPTT(pttPlayback, false) // and ends
	ta.syncPTTKeys()              // an app switch
	ta.tapPTT(input.Combo{vkPause})
	if got := ta.sentSince(); got != "" {
		t.Fatalf("while the user holds the key Resound sent %q", got)
	}
	ta.onTalk(false, 0)
	if got := ta.sentSince(); got != "" {
		t.Fatalf("at the user's release: %q, want nothing yet", got)
	}
	ta.fire()
	if got := ta.sentSince(); got != "+Pause" || ta.ptt.pressed == nil {
		t.Fatalf("after the release: %q, want push-to-talk pressed again", got)
	}
	ta.setPTT(pttTalk, false)
	if got := ta.sentSince(); got != "-Pause" {
		t.Fatalf("end: %q", got)
	}
}

// Pressing again before Resound pressed push-to-talk again: the first
// release's timer does nothing; only the last release's presses.
func TestTalkPressedAgain(t *testing.T) {
	ta := newTalkApp(t)
	ta.setPTT(pttTalk, true)
	ta.sentSince()
	ta.onTalk(true, 0)
	ta.onTalk(false, 0)
	ta.onTalk(true, 0)
	ta.fire()
	if got := ta.sentSince(); got != "" {
		t.Fatalf("pressed again before the timer: sent %q", got)
	}
	ta.onTalk(false, 0)
	ta.fire()
	if got := ta.sentSince(); got != "+Pause" {
		t.Fatalf("after the last release: %q", got)
	}
}

// With several push-to-talk combinations held, the user's own takes over
// its keys; the others go up as for a pause, and all come back after.
func TestTalkSeveralCombos(t *testing.T) {
	ta := newTalkApp(t)
	ta.cfg.PushToTalkExtra = [][]uint16{{vkScroll}}
	ta.setPTT(pttTalk, true)
	if got := ta.sentSince(); got != "+Pause + Scroll Lock" {
		t.Fatalf("hold: %q", got)
	}
	ta.onTalk(true, 1) // the user holds Scroll Lock
	if got := ta.sentSince(); got != "-Pause" {
		t.Fatalf("user's press: %q, want only Pause let go", got)
	}
	ta.onTalk(false, 1)
	ta.fire()
	if got := ta.sentSince(); got != "+Pause + Scroll Lock" {
		t.Fatalf("after: %q", got)
	}
}

// A talk with nothing held changes nothing, and Resound's hold that starts
// while the user talks waits for their release.
func TestTalkWithoutHold(t *testing.T) {
	ta := newTalkApp(t)
	ta.onTalk(true, 0)
	ta.setPTT(pttTalk, true)
	if got := ta.sentSince(); got != "" {
		t.Fatalf("while talking: %q", got)
	}
	ta.onTalk(false, 0)
	ta.fire()
	if got := ta.sentSince(); got != "+Pause" {
		t.Fatalf("after: %q", got)
	}
}

// The watcher closing mid-talk (its release never comes) leaves Resound
// free to press push-to-talk again.
func TestTalkWatcherClosed(t *testing.T) {
	ta := newTalkApp(t)
	ta.onTalk(true, 0)
	ta.closeHotkeys()
	ta.setPTT(pttTalk, true)
	if got := ta.sentSince(); got != "+Pause" {
		t.Fatalf("after closing: %q", got)
	}
}

// The hook gets the push-to-talk keys of each app as talk keys, only while
// the switch is on and Resound holds push-to-talk for sounds, and never
// while a key is recorded.
func TestTalkBindings(t *testing.T) {
	ta := newTalkApp(t)
	talk := func(app string) string {
		var names []string
		for _, c := range ta.bindingsFor(app).Talk {
			names = append(names, c.String())
		}
		return strings.Join(names, " · ")
	}
	if got := talk(""); got != "Pause" {
		t.Errorf("global: %q", got)
	}
	if got := talk("game.exe"); got != "Mouse 4" {
		t.Errorf("game: %q", got)
	}
	if k := ta.keymap(); k.Empty() || !k.Global.UsesMouse() && !k.Apps["game.exe"].UsesMouse() {
		t.Errorf("keymap %+v: the watcher must run, with the mouse hook for the game", k)
	}
	for name, change := range map[string]func(){
		"switch off":            func() { ta.cfg.NoPushToTalkPauses = true },
		"no hold while playing": func() { ta.cfg.PushToTalkWhilePlaying = false },
		"recording":             func() { ta.ptt.capturing = true },
	} {
		ta.cfg.NoPushToTalkPauses, ta.cfg.PushToTalkWhilePlaying, ta.ptt.capturing = false, true, false
		change()
		if got := talk(""); got != "" {
			t.Errorf("%s: talk keys %q", name, got)
		}
	}
	ta.cfg.NoPushToTalkPauses, ta.cfg.PushToTalkWhilePlaying = false, true
	ta.cfg.AppKeybinds[0].PushToTalk.Off, ta.cfg.AppKeybinds[0].PushToTalk.Keys = true, nil
	if got := talk("game.exe"); got != "" {
		t.Errorf("game with push-to-talk off: %q", got)
	}
	if len(ta.keymap().Apps["game.exe"].Talk) != 0 {
		t.Error("the keymap's game has talk keys")
	}
}

// A pause held by a pause key and the user's push-to-talk together lasts
// until both are let go, in either order.
func TestTalkWithPauseKey(t *testing.T) {
	for _, talkFirst := range []bool{true, false} {
		ta := newTalkApp(t)
		ta.onPauseKey(true)
		ta.onTalk(true, 0)
		if talkFirst {
			ta.onTalk(false, 0)
		} else {
			ta.onPauseKey(false)
		}
		if !ta.playback.holdLive {
			t.Fatalf("talk let go first %v: the pause ended with one of them still held", talkFirst)
		}
		if talkFirst {
			ta.onPauseKey(false)
		} else {
			ta.onTalk(false, 0)
		}
		ta.fire()
		if ta.playback.holdLive || ta.ptt.userTalk || ta.ptt.pauseKey {
			t.Fatalf("talk let go first %v: still paused after both", talkFirst)
		}
	}
}

// holdingBadge is the label of the cable's push-to-talk badge, "" for none.
func (ta *talkApp) holdingBadge() string {
	if b := ta.cableLine().Badge; b != nil && strings.HasPrefix(b.Label, "Holding") {
		return b.Label
	}
	return ""
}

// The flow shows push-to-talk held exactly while Resound holds it: never in
// an app that turned push-to-talk off, and with the keys of the app in
// front after a switch.
func TestHoldingBadgeFollowsKeys(t *testing.T) {
	ta := newTalkApp(t)
	ta.cfg.AppKeybinds[0].PushToTalk = &config.KeyOverride{Off: true}
	ta.fgApp = "game.exe"
	ta.setPTT(pttTalk, true)
	if got, badge := ta.sentSince(), ta.holdingBadge(); got != "" || badge != "" {
		t.Fatalf("push-to-talk off in the game: sent %q, badge %q", got, badge)
	}
	ta.setForegroundApp("plain.exe")
	if got, badge := ta.sentSince(), ta.holdingBadge(); got != "+Pause" || badge != "Holding Pause" {
		t.Fatalf("in an app with the global keys: sent %q, badge %q", got, badge)
	}
	ta.setForegroundApp("game.exe")
	if got, badge := ta.sentSince(), ta.holdingBadge(); got != "-Pause" || badge != "" {
		t.Fatalf("back in the game: sent %q, badge %q", got, badge)
	}
}

// While the user holds push-to-talk themselves, Resound holds nothing and
// shows nothing held.
func TestHoldingBadgeWhileUserTalks(t *testing.T) {
	ta := newTalkApp(t)
	ta.setPTT(pttTalk, true)
	ta.onTalk(true, 0)
	if badge := ta.holdingBadge(); badge != "" {
		t.Fatalf("badge %q while the user holds the key", badge)
	}
}
