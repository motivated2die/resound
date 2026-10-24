package app

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
	"github.com/motivated2die/resound/internal/ui"
)

// voiceMeta links a playing voice to the library sound it came from.
type voiceMeta struct {
	soundID uint64 // 0 for test sounds
	name    string
	preview bool          // plays for the user only ("You hear")
	length  time.Duration // as known when it started (0 = unknown): whether it mutes the mic (muteFor)
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
	// holdResuming: the pause key was let go and the sounds resume after
	// holdResumeDelay, unless holdGen changed meanwhile.
	holdResuming bool
	holdGen      int

	vmu    sync.Mutex
	voices map[uint64]voiceMeta

	last      *audio.PlayInfo
	delays    map[string]time.Duration
	lastError string // why the last test play failed; output failures come from the engine (outputErrorRows)
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
// push-to-talk is pressed and, for a sound long enough (muteFor), the mic
// muted first — after decoding, so neither happens early. Runs on the UI
// thread.
func (a *App) start(p *audio.Prepared, opts audio.PlayOptions, meta voiceMeta, forOthers bool) (audio.PlayInfo, error) {
	pb := &a.playback
	meta.length = p.Length()
	if forOthers {
		a.holdPlayback(a.muteFor(meta.length))
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
	if _, ok := a.takeHold(); ok {
		pb.wasHolding = false // nothing is left to resume
	}
	a.engine.StopAll() // emits EventFinished per voice, which releases the mic
}

// togglePause pauses every playing voice, or resumes the ones it paused.
// It returns true when playback is now paused.
func (a *App) togglePause() bool {
	pb := &a.playback
	if ids, ok := a.takeHold(); ok { // the phone keeps the pause the key was ending
		pb.paused, pb.pausedActive = ids, true
		logx.Printf("paused %d voice(s), taken over from the pause key", len(ids))
		return true
	}
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
	case on && !pb.talkLive && pb.holdResuming:
		a.setPTT(pttTalk, true)
		pb.talk, _ = a.takeHold() // talk-through keeps the pause the key was ending
		pb.talkLive = true
		logx.Printf("talk-through on, taken over from the pause key")
	case !on && pb.talkLive:
		a.resume(pb.talk)
		a.setPTT(pttTalk, false)
		pb.talk, pb.talkLive = nil, false
		logx.Printf("talk-through off")
	}
}

// holdResumeDelay is how long sounds stay paused after a pause key is let
// go, before they resume and push-to-talk goes down again. Pressed right
// after the game saw the pause key (often its own push-to-talk for another
// channel) go up, Resound's push-to-talk was sometimes missed by games.
const holdResumeDelay = 150 * time.Millisecond

// setHoldPause pauses playback and lets go of the mic and push-to-talk
// while a pause hotkey is held, so the user can talk on another channel
// (often the game's other push-to-talk key). on false ends the pause at
// once; letting go of the key ends it after a moment (releaseHoldPause).
func (a *App) setHoldPause(on bool) {
	pb := &a.playback
	switch {
	case on && pb.holdResuming: // pressed again before resuming: stay paused
		pb.holdResuming = false
		pb.holdGen++
		was := pb.wasHolding
		pb.hold = append(pb.hold, a.pauseActive()...) // and sounds started meanwhile
		pb.wasHolding = pb.wasHolding || was
	case on && !pb.holdLive && !pb.talkLive && !pb.pausedActive:
		pb.hold = a.pauseActive()
		pb.holdLive = true
		if len(pb.hold) > 0 {
			logx.Printf("hotkey: paused %d voice(s) while the pause key is held", len(pb.hold))
		}
	case !on && pb.holdLive:
		a.endHoldPause()
	}
}

// releaseHoldPause resumes the sounds holdResumeDelay after the pause key
// was let go, unless it is pressed again or another pause takes over
// meanwhile. With nothing to resume, the pause ends at once.
func (a *App) releaseHoldPause() {
	pb := &a.playback
	if !pb.holdLive || pb.holdResuming {
		return
	}
	if len(pb.hold) == 0 {
		a.endHoldPause()
		return
	}
	pb.holdResuming = true
	pb.holdGen++
	gen := pb.holdGen
	time.AfterFunc(holdResumeDelay, func() {
		a.tray.Post(func() {
			if pb.holdResuming && pb.holdGen == gen {
				a.endHoldPause()
				a.render()
			}
		})
	})
}

// endHoldPause resumes the sounds a pause key paused, now.
func (a *App) endHoldPause() {
	pb := &a.playback
	if len(pb.hold) > 0 {
		logx.Printf("hotkey: pause key released, resuming")
	}
	pb.holdResuming = false
	pb.holdGen++
	a.resume(pb.hold)
	pb.hold, pb.holdLive = nil, false
}

// takeHold ends a pause key's pause that is about to resume without
// resuming it, and returns its voices for another pause to carry on
// (wasHolding stays). ok is false when no such pause is pending.
func (a *App) takeHold() (ids []uint64, ok bool) {
	pb := &a.playback
	if !pb.holdResuming {
		return nil, false
	}
	ids = pb.hold
	pb.hold, pb.holdLive, pb.holdResuming = nil, false, false
	pb.holdGen++
	return ids, true
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
		a.holdPlayback(false) // the resumed voices count again (longPlaying)
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
		a.importPreviewEnded(ev.VoiceID)
		if n, ok := pb.remaining[ev.VoiceID]; ok {
			if n <= 1 {
				delete(pb.remaining, ev.VoiceID)
				pb.dropVoice(ev.VoiceID)
			} else {
				pb.remaining[ev.VoiceID] = n - 1
			}
		}
		switch {
		case len(pb.remaining) == 0:
			a.releasePlayback()
			a.render() // the flow's mic and push-to-talk badges go
		case pb.mutedMic != "" && !a.longPlaying():
			// Only shorter sounds are left: they leave the mic open.
			a.releaseMic()
			a.render()
		}
	case audio.EventOutputFailed:
		logx.Printf("%s output failed: %v", ev.Output, ev.Err) // shown under the flow from the engine's state
		// Voices on a dead output never finish; don't keep the mic muted.
		clear(pb.remaining)
		pb.vmu.Lock()
		clear(pb.voices)
		pb.vmu.Unlock()
		a.releasePlayback()
		a.render()
	}
}

// holdPlayback is what happens while sounds play for others, each if
// enabled: push-to-talk is held, and the mic muted while a sound long
// enough plays (longPlaying). long says that the sound about to start is
// one; it isn't playing yet.
func (a *App) holdPlayback(long bool) {
	if long || a.longPlaying() {
		a.holdMic()
	}
	a.setPTT(pttPlayback, a.cfg.PushToTalkWhilePlaying)
}

// muteFor reports whether a sound of this length mutes the mic: one longer
// than the threshold (MuteMicMinSeconds), or of unknown length (0), as
// muting was before there was a threshold. A threshold of 0 mutes for
// every sound.
func (a *App) muteFor(length time.Duration) bool {
	return muteFor(length, a.cfg.MuteMicMinSeconds)
}

func muteFor(length time.Duration, minSeconds float64) bool {
	return minSeconds <= 0 || length <= 0 || length.Seconds() > minSeconds
}

// longPlaying reports whether a sound that mutes the mic (muteFor) plays
// for others right now: started, not finished, not paused. Each sound
// counts on its own, so short sounds one after another never add up.
func (a *App) longPlaying() bool {
	pb := &a.playback
	for _, v := range a.engine.Voices() {
		if _, live := pb.remaining[v.ID]; !live || v.Paused {
			continue
		}
		if m, ok := pb.voice(v.ID); ok && !m.preview && a.muteFor(m.length) {
			return true
		}
	}
	return false
}

// syncMic mutes or unmutes the mic for what plays now, after the mute
// settings changed: turned off, the mic is let go at once.
func (a *App) syncMic() {
	if a.cfg.MuteMicWhilePlaying && a.longPlaying() {
		a.holdMic()
	} else {
		a.releaseMic()
	}
}

// releasePlayback undoes holdPlayback.
func (a *App) releasePlayback() {
	a.releaseMic()
	a.setPTT(pttPlayback, false)
}

// holdMic mutes the microphone for the duration of playback, if enabled.
func (a *App) holdMic() {
	pb := &a.playback
	if !a.cfg.MuteMicWhilePlaying || pb.mutedMic != "" || a.route.Mic == nil || a.ptt.userTalk {
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
	a.monitorVolume.Store(int32(a.cfg.MonitorVolume))
	a.cableVolume.Store(int32(a.cfg.CableVolume))
	a.engine.SetVolume(audio.RoleMonitor, audio.Gain(a.cfg.MonitorVolume))
	a.engine.SetVolume(audio.RoleCable, audio.Gain(a.cfg.CableVolume))
}

// normGain is the gain that evens out a sound's loudness: 1 when
// normalization is off or the sound wasn't measured yet (or by an older
// measurement). Safe on any goroutine.
func (a *App) normGain(lv *library.Level) float32 {
	if !a.normalize.Load() || lv == nil || lv.Version != audio.LoudnessVersion {
		return 1
	}
	return audio.NormGain(lv.LUFS, lv.Peak)
}

// toggleNormalize turns loudness normalization on or off. Sounds that are
// playing keep their gain; the next plays use the new setting.
func (a *App) toggleNormalize() {
	a.cfg.NoNormalize = !a.cfg.NoNormalize
	a.normalize.Store(!a.cfg.NoNormalize)
	a.save()
	logx.Printf("loudness normalization: %v", !a.cfg.NoNormalize)
	a.render()
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
