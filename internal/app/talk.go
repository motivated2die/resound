package app

import (
	"slices"
	"time"

	"github.com/motivated2die/resound/internal/input"
	"github.com/motivated2die/resound/internal/logx"
)

// The user's own push-to-talk ("My push-to-talk pauses sounds", on unless
// config.NoPushToTalkPauses). Resound holds the game's push-to-talk keys
// while sounds play, and nothing stops the user from pressing the same key
// themselves to talk. Their release then ends push-to-talk in the game
// while Resound still believes it holds it, so the rest of the sound goes
// unheard. Instead, the user's key acts like a pause key:
//
//  1. The press reaches the game as it is (the hook never swallows it): the
//     game keeps transmitting, now for the user's voice. The keys the user
//     holds become theirs: Resound forgets them without a release (one
//     would cut the user off); keys of further push-to-talk combinations it
//     held go up, as for a pause. The sounds pause and the mic is let go
//     (setHoldPause, shared with pause keys).
//  2. While the user holds it, Resound presses and releases no push-to-talk
//     key, the hand-over is off (the hook checks the talk itself, so a pause
//     key pressed right after can't release the user's keys either), and
//     sounds started meanwhile play without muting the mic: the user talks.
//  3. Their release reaches the game and ends push-to-talk there.
//     holdResumeDelay later, as after a pause key, the paused sounds resume
//     and Resound presses push-to-talk again for whatever still wants it:
//     those sounds, ones started meanwhile, talk-through (afterTalk).
//     Pressing again before then keeps everything paused.
//
// The hook reports the keys of the app in the foreground (input.Bindings
// Talk), only while Resound would hold push-to-talk for sounds.

// afterUI runs fn on the UI thread d from now; tests run it themselves.
var afterUI = func(a *App, d time.Duration, fn func()) {
	time.AfterFunc(d, func() { a.tray.Post(fn) })
}

// talkPauses reports whether the user's own push-to-talk presses pause
// the sounds.
func (a *App) talkPauses() bool {
	return a.cfg.PushToTalkWhilePlaying && !a.cfg.NoPushToTalkPauses
}

// onTalk follows the user pressing (down) and letting go of one of the
// push-to-talk combinations themselves; index is the one the press
// completed, among pttCombos.
func (a *App) onTalk(down bool, index int) {
	p := &a.ptt
	if down {
		if p.userTalk {
			return
		}
		p.userTalk = true
		p.talkGen++
		a.reclaimPTT()
		if p.pressed != nil {
			mine := p.pressed // the keys the user holds; without index all of them stay down
			if combos := a.pttCombos(); index >= 0 && index < len(combos) {
				mine = combos[index]
			}
			var up input.Combo
			for _, vk := range p.pressed {
				if !slices.ContainsFunc(mine, func(k uint16) bool { return input.SameCombo(input.Combo{k}, input.Combo{vk}) }) {
					up = append(up, vk)
				}
			}
			if err := releaseKeys(up); err != nil {
				logx.Printf("push-to-talk release: %v", err)
			}
			logx.Printf("push-to-talk taken over by the user's key (%s)", input.Combo(mine))
			p.pressed, p.held = nil, nil
		}
		switch duck := a.talkDuck(); {
		case a.playback.holdLive || len(a.engine.Voices()) > 0 && duck == 0:
			a.setHoldPause(true)
			logx.Printf("hotkey: paused while the user holds push-to-talk")
		case len(a.engine.Voices()) > 0:
			a.duckTalk(duck) // the sounds play on, quieter
		}
		return
	}
	if !p.userTalk {
		return
	}
	p.userTalk = false
	if !p.pauseKey { // else the pause lasts until the pause key is let go too
		a.releaseHoldPause()
	}
	gen := p.talkGen
	afterUI(a, holdResumeDelay, func() {
		if !p.userTalk && p.talkGen == gen { // not pressed again meanwhile
			a.afterTalk()
			a.render()
		}
	})
}

// catchUpTalk starts the user's talk (onTalk) before its event arrives
// when the hook already knows about it, so Resound never sends a key the
// user has just started to hold: e.g. a sound ending a moment after their
// press would otherwise release push-to-talk under their finger. Called
// before any push-to-talk key is sent; the event itself then changes
// nothing.
func (a *App) catchUpTalk() {
	if a.ptt.userTalk || a.hotkeys == nil {
		return
	}
	if i := a.hotkeys.Talking(); i >= 0 {
		a.onTalk(true, i)
	}
}

// onPauseKey follows a pause key going down and up. A pause held by both a
// pause key and the user's push-to-talk lasts until both are let go.
func (a *App) onPauseKey(down bool) {
	a.ptt.pauseKey = down
	switch {
	case down:
		a.setHoldPause(true)
	case !a.ptt.userTalk:
		a.releaseHoldPause()
	}
}

// afterTalk runs holdResumeDelay after the user let go of push-to-talk,
// whose release ended it in the game: it resumes the sounds the talk
// paused (if the pause's own timer hasn't yet) and has Resound press
// push-to-talk again for what still wants it, muting the mic again for
// long sounds.
func (a *App) afterTalk() {
	a.endTalkDuck()
	if a.playback.holdResuming {
		a.endHoldPause()
	}
	if a.playingForOthers() {
		a.holdPlayback(false)
		return
	}
	a.setPTT(pttPlayback, false) // talk-through may still want the keys
}

// playingForOthers reports whether a sound plays for others right now:
// started, not finished, not paused, not a preview.
func (a *App) playingForOthers() bool {
	pb := &a.playback
	for _, v := range a.engine.Voices() {
		if _, live := pb.remaining[v.ID]; !live || v.Paused {
			continue
		}
		if m, ok := pb.voice(v.ID); ok && !m.preview {
			return true
		}
	}
	return false
}

func (a *App) togglePTTPauses() {
	a.cfg.NoPushToTalkPauses = !a.cfg.NoPushToTalkPauses
	a.save()
	logx.Printf("push-to-talk pauses sounds: %v", !a.cfg.NoPushToTalkPauses)
	a.applyHotkeys()
	a.render()
}
