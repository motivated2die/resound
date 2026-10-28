package app

import (
	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

// Ducking: what the user holding a push-to-talk key themselves does to the
// sounds (talk.go). By default they pause (setHoldPause); with ducking
// (config.PushToTalkDuck, per app too) they keep playing at that volume,
// 1–100% on the volume sliders' curve on top of every other volume, while
// the key is held: the user talks over them. Like a pause, the mic Resound
// muted for them is let go meanwhile. Letting go of the key brings them
// back to full volume together with Resound's push-to-talk (afterTalk), as
// the game ended transmission with the user's release anyway.

// duckFor returns the ducking that applies while app is in the foreground
// ("" = everywhere), and whether the app has its own.
func (a *App) duckFor(app string) (value int, own bool) {
	if e := a.appKeybinds(app); app != "" && e != nil && e.PushToTalkDuck != nil {
		return *e.PushToTalkDuck, true
	}
	return a.cfg.PushToTalkDuck, false
}

// duckSpec is the ducking slider of the push-to-talk card of app ("" =
// global); nil while the user's push-to-talk does nothing to the sounds
// (Resound doesn't hold it for them, or "My push-to-talk pauses sounds" is
// off).
func (a *App) duckSpec(app string) *ui.DuckSpec {
	if !a.talkPauses() || a.ptt.capturing {
		return nil
	}
	v, own := a.duckFor(app)
	return &ui.DuckSpec{
		Value:     v,
		Inherited: app != "" && !own,
		Info: "While you hold your push-to-talk key yourself, sounds pause (all the way left). " +
			"Slide the knob to the right to keep them playing while you talk, quieter the further left: " +
			"you talk over them, and they come back to full volume when you let go. Your other volumes still apply.",
		OnChange: func(v int, done bool) { a.setDuck(app, v, done) },
	}
}

// setDuck sets app's ducking ("" = global) while its knob is dragged,
// saving it once it is let go.
func (a *App) setDuck(app string, v int, done bool) {
	v = max(0, min(100, v))
	if app == "" {
		a.cfg.PushToTalkDuck = v
	} else if e := a.appKeybinds(app); e != nil {
		e.PushToTalkDuck = &v
	}
	if done {
		a.save()
		logx.Printf("keybinds: ducking %s: %s", map[bool]string{true: "everywhere", false: "in " + app}[app == ""], ui.DuckText(v))
	}
	a.render()
}

// talkDuck is the ducking that applies now, in the app in the foreground.
func (a *App) talkDuck() int {
	v, _ := a.duckFor(a.fgApp)
	return v
}

// duckTalk lowers the sounds to the ducking's volume while the user holds
// push-to-talk, and lets go of the mic Resound muted: the user talks.
func (a *App) duckTalk(v int) {
	pb := &a.playback
	if !pb.ducked {
		logx.Printf("hotkey: sounds at %d%% while the user holds push-to-talk", v)
	}
	pb.ducked = true
	a.engine.SetDuck(audio.Gain(v))
	a.releaseMic()
}

// endTalkDuck brings the sounds back to full volume.
func (a *App) endTalkDuck() {
	pb := &a.playback
	if !pb.ducked {
		return
	}
	pb.ducked = false
	a.engine.SetDuck(1)
	logx.Printf("hotkey: sounds at full volume again")
}
