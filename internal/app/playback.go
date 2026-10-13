package app

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
	"github.com/motivated2die/resound/internal/ui"
)

// voiceMeta links a playing voice to the library sound it came from.
type voiceMeta struct {
	soundID uint64 // 0 for test sounds
	name    string
	preview bool // plays for the user only ("You hear")
}

// playbackState tracks what is playing, for the latency readout, the
// phone's progress bars, pause/talk-through, and muting the microphone.
// Everything is UI-thread state except voices, which the server reads.
type playbackState struct {
	// remaining counts, per voice, the outputs that have not finished yet.
	remaining map[uint64]int
	// mutedMic is the capture device Resound muted, "" when none. A mic
	// the user muted themselves is never unmuted by Resound.
	mutedMic string
	// muteGen counts Resound's mute changes, so a routing check that read
	// the mic's mute state before one is discarded (flow.go).
	muteGen int

	// paused holds the voices paused by the play/pause toggle; talk the
	// voices paused by talk-through. wasHolding remembers that mic/PTT were
	// held for them, so resuming holds them again.
	// hold the voices paused while a pause hotkey is held (hotkeys.go).
	// Only one of the three pauses is active at a time.
	paused, talk, hold               []uint64
	pausedActive, talkLive, holdLive bool
	wasHolding                       bool

	vmu    sync.Mutex
	voices map[uint64]voiceMeta

	last      *audio.PlayInfo
	delays    map[string]time.Duration
	lastError string
}

func (p *playbackState) init() {
	p.remaining = map[uint64]int{}
	p.delays = map[string]time.Duration{}
	p.voices = map[uint64]voiceMeta{}
}

func (p *playbackState) setVoice(id uint64, m voiceMeta) {
	p.vmu.Lock()
	p.voices[id] = m
	p.vmu.Unlock()
}

func (p *playbackState) voice(id uint64) (voiceMeta, bool) {
	p.vmu.Lock()
	defer p.vmu.Unlock()
	m, ok := p.voices[id]
	return m, ok
}

func (p *playbackState) dropVoice(id uint64) {
	p.vmu.Lock()
	delete(p.voices, id)
	p.vmu.Unlock()
}

// start queues a prepared sound. When forOthers is true (not a preview),
// the mic is muted and push-to-talk pressed first — after decoding, so
// neither happens early. Runs on the UI thread.
func (a *App) start(p *audio.Prepared, opts audio.PlayOptions, meta voiceMeta, forOthers bool) (audio.PlayInfo, error) {
	pb := &a.playback
	if forOthers {
		a.holdPlayback()
	}
	info, err := a.engine.Start(p, opts)
	if err != nil {
		if len(pb.remaining) == 0 {
			a.releasePlayback()
		}
		return info, err
	}
	pb.remaining[info.VoiceID] = info.Outputs
	meta.preview = !forOthers
	pb.setVoice(info.VoiceID, meta)
	a.animate()
	logx.Printf("play #%d %q (%v) on %d output(s) with %d decoder(s), ready in %v",
		info.VoiceID, info.Name, info.Length.Round(time.Millisecond), info.Outputs, info.Decoders, info.Decode.Round(100*time.Microsecond))
	return info, nil
}

// playTest plays the tone or the test sound from the panel.
func (a *App) playTest(tone bool) {
	pb := &a.playback
	pb.lastError = ""
	clear(pb.delays)
	var (
		p   *audio.Prepared
		err error
	)
	if tone {
		p, err = a.engine.PrepareTone()
	} else {
		p, err = a.engine.PrepareFile(a.cfg.TestSoundPath)
	}
	var info audio.PlayInfo
	if err == nil {
		info, err = a.start(p, audio.PlayOptions{}, voiceMeta{name: "test"}, true)
	}
	if err != nil {
		logx.Printf("play failed: %v", err)
		pb.lastError = err.Error()
		pb.last = nil
	} else {
		pb.last = &info
	}
	a.render()
}

func (a *App) stopAll() {
	pb := &a.playback
	pb.paused, pb.pausedActive = nil, false
	a.engine.StopAll() // emits EventFinished per voice, which releases the mic
}

// togglePause pauses every playing voice, or resumes the ones it paused.
// It returns true when playback is now paused.
func (a *App) togglePause() bool {
	pb := &a.playback
	if pb.talkLive || pb.holdLive {
		return pb.pausedActive // talk-through or a pause key owns the pause right now
	}
	if !pb.pausedActive {
		pb.paused = a.pauseActive()
		pb.pausedActive = true
		logx.Printf("paused %d voice(s)", len(pb.paused))
		return true
	}
	a.resume(pb.paused)
	pb.paused, pb.pausedActive = nil, false
	return false
}

// setTalkThrough pauses playback, frees the mic and holds push-to-talk
// while the user holds the talk button on the phone. The PTT reasons are
// switched in an order that keeps the key down across the hand-over.
func (a *App) setTalkThrough(on bool) {
	pb := &a.playback
	switch {
	case on && !pb.talkLive && !pb.pausedActive && !pb.holdLive:
		a.setPTT(pttTalk, true)
		pb.talk = a.pauseActive()
		pb.talkLive = true
		logx.Printf("talk-through on, paused %d voice(s)", len(pb.talk))
	case !on && pb.talkLive:
		a.resume(pb.talk)
		a.setPTT(pttTalk, false)
		pb.talk, pb.talkLive = nil, false
		logx.Printf("talk-through off")
	}
}

// setHoldPause pauses playback and lets go of the mic and push-to-talk
// while a pause hotkey is held, so the user can talk on another channel
// (often the game's other push-to-talk key). Releasing it resumes both.
func (a *App) setHoldPause(on bool) {
	pb := &a.playback
	switch {
	case on && !pb.holdLive && !pb.talkLive && !pb.pausedActive:
		pb.hold = a.pauseActive()
		pb.holdLive = true
		if len(pb.hold) > 0 {
			logx.Printf("hotkey: paused %d voice(s) while the pause key is held", len(pb.hold))
		}
	case !on && pb.holdLive:
		if len(pb.hold) > 0 {
			logx.Printf("hotkey: pause key released, resuming")
		}
		a.resume(pb.hold)
		pb.hold, pb.holdLive = nil, false
	}
}

// pauseActive pauses all running voices, lets go of the mic and the
// playback push-to-talk, and returns the paused voice IDs.
func (a *App) pauseActive() []uint64 {
	var ids []uint64
	for _, v := range a.engine.Voices() {
		if !v.Paused {
			a.engine.SetPaused(v.ID, true)
			ids = append(ids, v.ID)
		}
	}
	a.playback.wasHolding = a.playback.mutedMic != "" || a.ptt.reasons[pttPlayback]
	a.releasePlayback()
	return ids
}

func (a *App) resume(ids []uint64) {
	pb := &a.playback
	resumed := 0
	for _, id := range ids {
		if _, live := pb.remaining[id]; live {
			a.engine.SetPaused(id, false)
			resumed++
		}
	}
	if resumed > 0 && pb.wasHolding {
		a.holdPlayback()
	}
	pb.wasHolding = false
}

func (a *App) onEngineEvent(ev audio.Event) {
	pb := &a.playback
	switch ev.Kind {
	case audio.EventFirstBuffer:
		logx.Printf("voice #%d first buffer on %s after %v", ev.VoiceID, ev.Output, ev.Delay.Round(100*time.Microsecond))
		if pb.last != nil && pb.last.VoiceID == ev.VoiceID {
			pb.delays[ev.Output] = ev.Delay
			a.render()
		}
	case audio.EventFinished:
		if n, ok := pb.remaining[ev.VoiceID]; ok {
			if n <= 1 {
				delete(pb.remaining, ev.VoiceID)
				pb.dropVoice(ev.VoiceID)
			} else {
				pb.remaining[ev.VoiceID] = n - 1
			}
		}
		if len(pb.remaining) == 0 {
			a.releasePlayback()
		}
	case audio.EventOutputFailed:
		logx.Printf("%s output failed: %v", ev.Output, ev.Err)
		pb.lastError = fmt.Sprintf("%s output stopped: %v", ev.Output, ev.Err)
		// Voices on a dead output never finish; don't keep the mic muted.
		clear(pb.remaining)
		pb.vmu.Lock()
		clear(pb.voices)
		pb.vmu.Unlock()
		a.releasePlayback()
		a.render()
	}
}

// holdPlayback is what happens while sounds play for others: the mic is
// muted and push-to-talk held, each if enabled.
func (a *App) holdPlayback() {
	a.holdMic()
	a.setPTT(pttPlayback, a.cfg.PushToTalkWhilePlaying)
}

// releasePlayback undoes holdPlayback.
func (a *App) releasePlayback() {
	a.releaseMic()
	a.setPTT(pttPlayback, false)
}

// holdMic mutes the microphone for the duration of playback, if enabled.
func (a *App) holdMic() {
	pb := &a.playback
	if !a.cfg.MuteMicWhilePlaying || pb.mutedMic != "" || a.route.Mic == nil {
		return
	}
	id := a.route.Mic.ID
	start := time.Now()
	if muted, err := routing.IsMuted(id); err != nil || muted {
		return // unknown state, or muted by the user: leave it alone
	}
	if err := routing.SetMute(id, true); err != nil {
		logx.Printf("muting mic: %v", err)
		return
	}
	pb.mutedMic = id
	a.setRouteMuted(id, true)
	logx.Printf("mic muted for playback in %v", time.Since(start).Round(100*time.Microsecond))
}

// releaseMic undoes holdMic.
func (a *App) releaseMic() {
	pb := &a.playback
	if pb.mutedMic == "" {
		return
	}
	if err := routing.SetMute(pb.mutedMic, false); err != nil {
		logx.Printf("unmuting mic: %v", err)
	} else {
		logx.Printf("mic unmuted")
		a.setRouteMuted(pb.mutedMic, false)
	}
	pb.mutedMic = ""
}

// setRouteMuted records a mute change Resound made in the routing state, so
// the panel doesn't show the mute Windows reported before it (until the
// next check) as the user's own.
func (a *App) setRouteMuted(id string, muted bool) {
	a.playback.muteGen++
	for i := range a.route.Inputs {
		if a.route.Inputs[i].ID == id {
			a.route.Inputs[i].Muted = muted
		}
	}
	if a.route.Mic != nil && a.route.Mic.ID == id {
		a.route.Mic.Muted = muted
	}
}

func (a *App) chooseTestSound() {
	defer a.reopenPanel()
	var patterns []string
	for _, ext := range audio.SupportedExtensions {
		patterns = append(patterns, "*"+ext)
	}
	path, ok, err := ui.OpenFileDialog(a.tray.Hwnd(), "Choose a test sound", []ui.FileFilter{
		{Name: "Audio files", Pattern: strings.Join(patterns, ";")},
		{Name: "All files", Pattern: "*.*"},
	})
	if err != nil {
		logx.Printf("file dialog: %v", err)
		return
	}
	if !ok {
		return
	}
	a.cfg.TestSoundPath = path
	a.save()
	logx.Printf("test sound: %s", path)
}

// setVolume applies a role's volume at once; done saves it (the end of a
// slider drag, or a button click).
func (a *App) setVolume(role string, v int, done bool) {
	p := &a.cfg.MonitorVolume
	if role == audio.RoleCable {
		p = &a.cfg.CableVolume
	}
	*p = max(0, min(100, v))
	a.applyVolumes()
	if done {
		a.save()
	}
	a.render()
}

func (a *App) applyVolumes() {
	a.engine.SetVolume(audio.RoleMonitor, audio.Gain(a.cfg.MonitorVolume))
	a.engine.SetVolume(audio.RoleCable, audio.Gain(a.cfg.CableVolume))
}

// --- home page playback status ---

// playingStatus is the status line while sounds play: "Playing · Airhorn",
// "Playing 2 sounds" or "Paused". ok is false when nothing plays.
func (a *App) playingStatus() (text string, ok bool) {
	voices := a.engine.Voices()
	if len(voices) == 0 {
		return "", false
	}
	paused := 0
	for _, v := range voices {
		if v.Paused {
			paused++
		}
	}
	switch {
	case paused == len(voices):
		return "Paused", true
	case len(voices) > 1:
		return fmt.Sprintf("Playing %d sounds", len(voices)), true
	}
	if meta, ok := a.playback.voice(voices[0].ID); ok && meta.name != "" && meta.name != "test" {
		return "Playing · " + meta.name, true
	}
	return "Playing", true
}

// playbackProgress is the furthest progress of the playing sounds (0..1),
// or -1 when nothing plays. The home page header reads it on every paint.
func (a *App) playbackProgress() float64 {
	best := -1.0
	for _, v := range a.engine.Voices() {
		if v.Length > 0 {
			best = max(best, min(1, float64(v.Position)/float64(v.Length)))
		} else {
			best = max(best, 0)
		}
	}
	return best
}

// migrateVolumeScale converts volumes saved on the linear scale of versions
// before 0.9 to the loudness curve (audio.Gain), so everything sounds as
// loud as before: the two defaults and every per-sound volume. Runs once;
// again only after restoring a backup from such a version.
func (a *App) migrateVolumeScale() {
	if a.cfg.VolumeScale != 0 {
		return
	}
	before := [2]int{a.cfg.MonitorVolume, a.cfg.CableVolume}
	a.cfg.MonitorVolume = audio.FromLinear(a.cfg.MonitorVolume)
	a.cfg.CableVolume = audio.FromLinear(a.cfg.CableVolume)
	n, err := a.lib.MapVolumes(audio.FromLinear)
	if err != nil {
		logx.Printf("volumes: saving the converted sound volumes: %v", err)
	}
	a.cfg.VolumeScale = 1
	a.save()
	logx.Printf("volumes: converted to the loudness curve: you hear %d%% → %d%%, others hear %d%% → %d%%, %d sound(s)",
		before[0], a.cfg.MonitorVolume, before[1], a.cfg.CableVolume, n)
}
