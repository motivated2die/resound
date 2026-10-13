package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
	"github.com/motivated2die/resound/internal/ui"
)

// The audio flow (ui.AudioFlow) shows where audio goes: the microphone and
// the soundboard at the top; the mic and the soundboard's cable stream plug
// into VB-Cable, whose far end ("CABLE Output") is what others hear; the
// soundboard's second stream goes to what the user hears (a preview only
// there). It is on the home page, the setup's audio steps and
// Settings → Audio & voice chat. Pipes break where the routing isn't set
// up, with a badge that explains why and leads to the fix; they show what
// Resound does while sounds play (mic muted, push-to-talk held, paused).
//
// While the flow is on screen, live level bars fill from:
//   - the microphone: recorded by Resound only to measure it (Windows meters
//     a capture device only while something records from it);
//   - the sounds and "You hear": Resound's own mixers;
//   - "Others hear": recorded from "CABLE Output", the cable's far end, the
//     same way: exactly what others' apps receive, the mic included.
//     (Windows' own meter of the cable's devices, which its Sound dialog
//     shows, decays over seconds after audio stopped; recording measures
//     the samples.)

// Meter ballistics: a bar jumps up at once and falls by flowFall per tick.
const (
	flowTick    = 33 * time.Millisecond
	flowFall    = 0.05
	flowRecheck = 3 * time.Second // routing re-read while the flow is visible
)

// levelState holds the meters while the flow is visible. UI thread only.
type levelState struct {
	mic      *audio.InputLevel
	micID    string            // what mic records from ("" = not open)
	micErr   error             // why the mic meter couldn't open; not retried for micID
	cable    *audio.InputLevel // records CABLE Output to measure it
	cableID  string
	cableErr error

	// Shown bar positions (0..1, decibel scale), falling smoothly.
	micLvl, sounds, monitor, cableSend, others float64

	lastCheck time.Time // last routing re-read
	checking  bool      // a routing re-read is running
}

// flowLevel returns a function reading one shown level, for the rows.
func (a *App) flowLevel(p *float64) func() float64 { return func() float64 { return *p } }

// syncLevels opens, switches or closes the meters to match the chosen
// devices. Opening takes a few milliseconds; it only happens when a device
// changes.
func (a *App) syncLevels() {
	l := &a.levels
	micID := ""
	if a.route.Mic != nil {
		micID = a.route.Mic.ID
	}
	if micID != l.micID {
		a.closeMicLevel()
		l.micID, l.micErr = micID, nil
		if micID != "" {
			start := time.Now()
			l.mic, l.micErr = audio.OpenInputLevel(micID)
			if l.micErr != nil {
				logx.Printf("mic level: %v", l.micErr)
				defer a.render() // say why the bar stays empty
			} else {
				logx.Printf("mic level: recording to measure in %v", time.Since(start).Round(100*time.Microsecond))
			}
		}
	}
	cableID := ""
	if a.route.CableOutput != nil && a.cableSelected() {
		cableID = a.route.CableOutput.ID
	}
	if cableID != l.cableID {
		if l.cable != nil {
			l.cable.Close()
			l.cable = nil
		}
		l.cableID, l.cableErr = cableID, nil
		if cableID != "" {
			if l.cable, l.cableErr = audio.OpenInputLevel(cableID); l.cableErr != nil {
				logx.Printf("cable level: %v", l.cableErr)
			}
		}
	}
}

func (a *App) closeMicLevel() {
	l := &a.levels
	if l.mic != nil {
		l.mic.Close()
		l.mic = nil
		logx.Printf("mic level: stopped recording")
	}
	l.micID, l.micErr = "", nil
}

// closeLevels stops every meter (the flow left the screen).
func (a *App) closeLevels() {
	a.closeMicLevel()
	l := &a.levels
	if l.cable != nil {
		l.cable.Close()
		l.cable = nil
	}
	l.cableID = ""
	l.micLvl, l.sounds, l.monitor, l.cableSend, l.others = 0, 0, 0, 0, 0
}

// takeLevels reads the peaks since the last tick into the shown levels.
func (a *App) takeLevels() {
	l := &a.levels
	fall := func(shown *float64, peak float32) {
		*shown = max(audio.MeterPosition(peak), *shown-flowFall)
	}
	if l.mic != nil {
		peak, err := l.mic.Take()
		if err != nil {
			logx.Printf("mic level: %v", err)
			l.mic.Close()
			l.mic, l.micErr = nil, err
		}
		fall(&l.micLvl, peak)
	} else {
		fall(&l.micLvl, 0)
	}
	lv := a.engine.Levels()
	fall(&l.sounds, lv.Sounds)
	fall(&l.monitor, lv.Monitor)
	fall(&l.cableSend, lv.Cable)
	if l.cable != nil {
		peak, err := l.cable.Take()
		if err != nil {
			logx.Printf("cable level: %v", err)
			l.cable.Close()
			l.cable, l.cableErr = nil, err
		}
		fall(&l.others, peak)
	} else {
		fall(&l.others, lv.Cable) // another output than VB-Cable: Resound's part
	}
}

// recheckRouting re-reads the routing every few seconds while the flow is
// visible, so changes made in Windows' own dialogs show up. The check runs
// off the UI thread (listing devices can be slow, e.g. Bluetooth); the page
// renders again only when something the flow shows changed.
func (a *App) recheckRouting() {
	l := &a.levels
	if l.checking || a.busy != "" || time.Since(l.lastCheck) < flowRecheck {
		return
	}
	l.checking = true
	micID, gen := a.cfg.MicDeviceID, a.playback.muteGen
	go func() {
		st, err := routing.Check(micID)
		a.tray.Post(func() {
			l.checking, l.lastCheck = false, time.Now()
			if a.cfg.MicDeviceID != micID || a.busy != "" {
				return // changed meanwhile; that refreshed the routing itself
			}
			if a.playback.muteGen != gen {
				// Resound muted or unmuted the mic while this ran: its mute
				// state is outdated. Check again on the next tick.
				l.lastCheck = time.Time{}
				return
			}
			if err != nil {
				logx.Printf("routing check: %v", err)
			}
			before := a.routeKey()
			a.route, a.routeErr = st, err
			if a.routeKey() != before {
				a.refreshDevices(false)
				a.syncCableOutput() // VB-Cable appeared or went
				a.render()
			}
		})
	}()
}

// routeKey summarizes what the flow shows of the routing.
func (a *App) routeKey() string {
	st := a.route
	mic := ""
	if st.Mic != nil {
		mic = fmt.Sprintf("%s/%v", st.Mic.ID, st.Mic.Muted)
	}
	return fmt.Sprintf("%v|%v|%s|%d|%v", st.CableInstalled(), st.MicRouted, mic, len(st.Inputs), a.routeErr != nil)
}

// --- the animation ticker ------------------------------------------------------------

// animate keeps the panel moving while it is open: the flow's level bars
// and pipes, and the home page's progress bar while sounds play. It
// repaints about 30 times a second and renders the page again when playback
// starts or stops (texts and buttons change). It ends by itself when the
// panel closes or nothing moves. UI thread.
func (a *App) animate() {
	if a.watching {
		return
	}
	a.watching = true
	go func() {
		t := time.NewTicker(flowTick)
		defer t.Stop()
		for range t.C {
			keep := make(chan bool, 1)
			a.tray.Post(func() { keep <- a.tick() })
			select {
			case k := <-keep:
				if !k {
					return
				}
			case <-time.After(time.Second): // UI thread gone (quitting)
				return
			}
		}
	}()
}

func (a *App) tick() bool {
	visible := a.tray.Popup().Visible()
	flow := visible && a.flowOnPage
	playing := len(a.engine.Voices()) > 0
	if flow {
		a.syncLevels()
		a.takeLevels()
		a.recheckRouting()
	} else {
		a.closeLevels()
	}
	switch {
	case playing != a.wasPlaying:
		a.wasPlaying = playing
		if visible {
			a.render()
		}
	case flow || (visible && playing && a.page == pageHome):
		a.tray.Popup().Repaint()
	}
	keep := flow || (visible && playing && a.page == pageHome)
	if !keep {
		a.watching = false
		a.closeLevels()
	}
	return keep
}

// --- building the flow -------------------------------------------------------------

// flowRows returns the audio flow's rows and marks the page as showing it,
// which keeps the meters running while the panel is open. setupLater is
// true on the setup step before voice chat: what voice chat still needs is
// shown as coming next instead of as a problem.
func (a *App) flowRows(setupLater bool) []ui.Row {
	a.flowOnPage = true
	rows := a.audioFlow(setupLater).Rows()
	if a.playback.lastError != "" {
		rows = append(rows, ui.Paragraph(a.playback.lastError, ui.ToneBad))
	}
	if a.devErr != nil {
		rows = append(rows, ui.Paragraph("Device list failed: "+a.devErr.Error(), ui.ToneBad))
	}
	if errors.Is(a.levels.micErr, audio.ErrMicBlocked) {
		rows = append(rows,
			ui.Paragraph("Your microphone's level can't be shown: Windows' privacy settings keep desktop apps from the microphone. "+
				"Voice chat still gets it through VB-Cable.", ui.ToneWarning),
			ui.ClosingButton("Open microphone privacy settings", ui.ButtonNormal, func() { _ = ui.ShellOpen("ms-settings:privacy-microphone") }))
	}
	return rows
}

// flowFix runs fix where the flow sits next to the controls it needs (the
// audio page, the setup's voice step); elsewhere a badge click opens the
// audio page, which explains and fixes it.
func (a *App) flowFix(fix func()) func() {
	return func() {
		if a.page == pageAudio || (a.page == pageWizard && a.wizardStep == wizardVoiceStep) {
			fix()
			return
		}
		a.openPage(pageAudio)
	}
}

func (a *App) outputStatus(role string) (audio.OutputStatus, bool) {
	for _, s := range a.engine.Status() {
		if s.Role == role {
			return s, true
		}
	}
	return audio.OutputStatus{}, false
}

func (a *App) audioFlow(setupLater bool) ui.AudioFlow {
	l := &a.levels
	f := ui.AudioFlow{
		Mic:    a.micDevice(),
		Sounds: a.soundsSource(),
		Cable:  a.cableEnd(),
		You:    a.youDevice(),
	}
	f.MicPipe = a.micPipe(setupLater)
	f.CablePipe = a.cablePipe(setupLater)
	f.CableLine = a.cableLine(setupLater)
	f.YouPipe = ui.Pipe{State: ui.PipeOn, Level: a.flowLevel(&l.monitor)}

	// Playback paused: by the phone's pause, talk-through or a pause key.
	// Both of the soundboard's streams stop; the badge sits on the cable's.
	pb := &a.playback
	if pb.pausedActive || pb.talkLive || pb.holdLive {
		label, tip := "Paused", "Playback is paused from the phone."
		switch {
		case pb.talkLive:
			label, tip = "Talking", "Talk-through: the sounds pause while you talk, push-to-talk stays held."
		case pb.holdLive:
			label, tip = "Pause key held", "The sounds pause while you hold a pause key, and your mic and push-to-talk are let go."
		}
		if f.CablePipe.State == ui.PipeOn {
			f.CablePipe = ui.Pipe{State: ui.PipeHeld, Badge: &ui.Badge{Icon: ui.IconPause, Label: label, Tip: tip}}
		}
		f.YouPipe = ui.Pipe{State: ui.PipeHeld}
	}
	if s, ok := a.outputStatus(audio.RoleMonitor); ok && !s.Running && s.Err != nil {
		f.YouPipe = ui.Pipe{State: ui.PipeBroken, Badge: &ui.Badge{
			Icon: ui.IconAlert, Label: "Output failed", Tone: ui.ToneBad,
			Tip:     "You hear nothing: " + s.Err.Error() + ". Click to restart the outputs, e.g. after plugging the device back in.",
			OnClick: a.restartOutputs,
		}}
	}
	return f
}

// micDevice is the microphone dropdown: automatic or a capture device
// (VB-Cable's own "CABLE Output" is never one).
func (a *App) micDevice() ui.FlowDevice {
	st := a.route
	auto := ui.Option{Label: "Automatic"}
	if st.Mic != nil && a.cfg.MicDeviceID == "" {
		auto.Label = "Automatic: " + st.Mic.Name
	}
	opts := []ui.Option{auto}
	ids := []string{""}
	sel := 0
	for _, in := range st.Inputs {
		if in.IsCableOutput() {
			continue
		}
		o := ui.Option{Label: in.Name}
		switch {
		case in.Muted:
			o.Detail = "muted"
		case in.Default:
			o.Detail = "default"
		}
		if in.ID == a.cfg.MicDeviceID {
			sel = len(opts)
		}
		opts = append(opts, o)
		ids = append(ids, in.ID)
	}
	d := ui.FlowDevice{ID: "mic", Caption: "Your microphone", Options: opts, Selected: sel,
		OnSelect: func(i int) { a.selectMic(ids[i]) }}
	switch {
	case errors.Is(a.levels.micErr, audio.ErrMicBlocked):
		d.Value = "level blocked"
	case a.levels.micErr != nil:
		d.Value = "no level"
	default:
		d.Level = a.flowLevel(&a.levels.micLvl)
	}
	return d
}

// soundsSource is the soundboard: what plays, and the test button.
func (a *App) soundsSource() ui.FlowSounds {
	s := ui.FlowSounds{Caption: "Soundboard", OnTest: func() { a.playTest(true) }, OnStop: a.stopAll,
		Level: a.flowLevel(&a.levels.sounds)}
	text, ok := a.playingStatus()
	if !ok {
		return s
	}
	s.Playing, s.Status = true, "Playing"
	switch {
	case text == "Paused":
		s.Status = "Paused"
	case a.onlyPreviews():
		s.Status = "Preview"
	}
	return s
}

// onlyPreviews reports whether everything playing is a preview, which only
// the user hears (the flow lights only the pipe to "You hear").
func (a *App) onlyPreviews() bool {
	for _, v := range a.engine.Voices() {
		if m, ok := a.playback.voice(v.ID); !ok || !m.preview {
			return false
		}
	}
	return true
}

// cableEnd is the cable's far end, "CABLE Output": the microphone others'
// apps listen to. It isn't a choice; the volume is the sounds' into the cable.
func (a *App) cableEnd() ui.FlowCable {
	c := ui.FlowCable{
		Caption: "Others hear", Value: fmt.Sprintf("%d%%", a.cfg.CableVolume),
		Level: a.flowLevel(&a.levels.others),
		Volume: &ui.FlowVolume{Value: a.cfg.CableVolume,
			Tip:      "Volume of your sounds for others. Your voice isn't changed.",
			OnChange: func(v int, done bool) { a.setVolume(audio.RoleCable, v, done) }},
	}
	switch out := a.route.CableOutput; {
	case out != nil && a.cableSelected():
		c.Name = "CABLE Output"
		c.Info = "Others hear you and your sounds through VB-Cable. In Discord, games or OBS, choose “" + out.Name + "” as your microphone."
	case out != nil:
		c.Name = a.cableTargetName()
		c.Info = "Your sounds for others go to “" + c.Name + "” (chosen in Settings → Audio & voice chat), not to VB-Cable, where your voice goes."
		c.OnClick = a.flowFix(func() {})
	default:
		c.Placeholder = "Not installed"
		c.Tip = "Others hear you and your sounds through VB-Cable, a free virtual audio driver by VB-Audio. Click to install it."
		c.OnClick = a.flowFix(a.installCable)
		if a.cable.state != cableIdle {
			c.Placeholder = a.cableStepInfo()
			c.OnClick = a.flowFix(func() {})
		}
	}
	return c
}

// cableTargetName is the name of the device "others hear" plays into.
func (a *App) cableTargetName() string {
	id := a.cableOutputID()
	for _, d := range a.devices {
		if d.ID == id {
			return d.Name
		}
	}
	return "another device"
}

// youDevice is the user's own output: a choice of devices.
func (a *App) youDevice() ui.FlowDevice {
	opts := []ui.Option{{Label: "System default"}}
	sel := 0
	for i, dev := range a.devices {
		o := ui.Option{Label: dev.Name}
		if dev.Default {
			o.Detail = "default"
		}
		opts = append(opts, o)
		if dev.ID == a.cfg.MonitorDeviceID {
			sel = i + 1
		}
	}
	return ui.FlowDevice{
		ID: "monitor", Caption: "You hear", Value: fmt.Sprintf("%d%%", a.cfg.MonitorVolume),
		Options: opts, Selected: sel,
		OnSelect: func(i int) {
			if i == 0 {
				a.selectMonitor("")
			} else {
				a.selectMonitor(a.devices[i-1].ID)
			}
		},
		Level:  a.flowLevel(&a.levels.monitor),
		Volume: &ui.FlowVolume{Value: a.cfg.MonitorVolume, Tip: "Volume of your sounds for you.", OnChange: func(v int, done bool) { a.setVolume(audio.RoleMonitor, v, done) }},
	}
}

// cableOutputDropdown is the advanced choice of where sounds for others go:
// automatically VB-Cable, or another device such as a virtual mixer's input.
func (a *App) cableOutputDropdown() ui.Row {
	auto := ui.Option{Label: "Automatic: VB-Cable", Detail: "recommended"}
	if a.route.CableInput == nil {
		auto.Label = "Automatic: VB-Cable (not installed)"
	}
	opts, ids := []ui.Option{auto}, []string{""}
	sel := 0
	for _, dev := range a.devices {
		if a.route.CableInput != nil && dev.ID == a.route.CableInput.ID {
			continue // that's the automatic choice
		}
		if dev.ID == a.cfg.CableDeviceID {
			sel = len(opts)
		}
		opts = append(opts, ui.Option{Label: dev.Name})
		ids = append(ids, dev.ID)
	}
	return ui.Dropdown("cable-target", "Sounds for others play into", "", opts, sel, func(i int) { a.selectCable(ids[i]) })
}

// micPipe is the microphone's way into the cable: Windows' "Listen to this
// device" on the mic, playing through CABLE Input.
func (a *App) micPipe(setupLater bool) ui.Pipe {
	st := a.route
	switch {
	case st.Mic == nil && a.cfg.MicDeviceID != "":
		return ui.Pipe{State: ui.PipeBroken, Badge: &ui.Badge{Icon: ui.IconUnplugged, Label: "Not connected", Tone: ui.ToneBad,
			Tip: "The chosen microphone is not connected. Plug it in, or choose another one above."}}
	case st.Mic == nil:
		return ui.Pipe{State: ui.PipeOff, Badge: &ui.Badge{Icon: ui.IconUnplugged, Label: "No microphone",
			Tip: "Windows has no recording device. Connect a microphone to talk in voice chat."}}
	case !st.CableInstalled():
		return ui.Pipe{State: ui.PipeOff} // the cable side explains
	case a.busy != "":
		return ui.Pipe{State: ui.PipeOff, Badge: &ui.Badge{Icon: ui.IconAlert, Label: "Waiting…", Tone: ui.ToneWarning,
			Tip: "Waiting for administrator approval to route your microphone."}}
	case !st.MicRouted && setupLater:
		return ui.Pipe{State: ui.PipeOff, Badge: &ui.Badge{Icon: ui.IconUnplugged, Label: "Next step",
			Tip: "Your voice is connected to voice chat in the next step."}}
	case !st.MicRouted:
		tip := "Others don't hear your voice: it doesn't reach the cable. Click to route it through Windows' “Listen to this device” (Windows asks for administrator approval once)."
		if st.Mic.Listening {
			tip = "Others don't hear your voice: Windows' “Listen to this device” plays your microphone somewhere else than VB-Cable. Click to route it into the cable (Windows asks for administrator approval once)."
		}
		return ui.Pipe{State: ui.PipeBroken, Badge: &ui.Badge{Icon: ui.IconAlert, Label: "Not routed", Tone: ui.ToneWarning, Tip: tip,
			OnClick: a.flowFix(func() { a.setMicRouting(true) })}}
	case a.playback.mutedMic != "":
		return ui.Pipe{State: ui.PipeHeld, Badge: &ui.Badge{Icon: ui.IconMicOff, Label: "Muted for sounds",
			Tip:     "Resound mutes your microphone while sounds play, so others hear them clearly. It unmutes when they end. Click to change this.",
			OnClick: a.flowFix(func() {})}}
	case st.Mic.Muted:
		return ui.Pipe{State: ui.PipeHeld, Badge: &ui.Badge{Icon: ui.IconMicOff, Label: "Muted", Tone: ui.ToneWarning,
			Tip: "Your microphone is muted in Windows, so others don't hear you. Unmute it in Windows or in your headset."}}
	}
	return ui.Pipe{State: ui.PipeOn, Level: a.flowLevel(&a.levels.micLvl)}
}

// cablePipe is the soundboard's stream into the cable: Resound's "others
// hear" output, automatically VB-Cable's CABLE Input.
func (a *App) cablePipe(setupLater bool) ui.Pipe {
	cable, _ := a.outputStatus(audio.RoleCable)
	switch {
	case a.cableOutputID() == "":
		return ui.Pipe{State: ui.PipeOff} // no VB-Cable: the cable explains
	case cable.Err != nil && !cable.Running:
		return ui.Pipe{State: ui.PipeBroken, Badge: &ui.Badge{Icon: ui.IconAlert, Label: "Output failed", Tone: ui.ToneBad,
			Tip:     "Others don't hear your sounds: " + cable.Err.Error() + ". Click to restart the outputs.",
			OnClick: a.restartOutputs}}
	case !a.cableSelected():
		return ui.Pipe{State: ui.PipeOff, Badge: &ui.Badge{Icon: ui.IconUnplugged, Label: "Elsewhere",
			Tip:     "Your sounds for others play into “" + a.cableTargetName() + "”, not into VB-Cable (chosen in Settings → Audio & voice chat).",
			OnClick: a.flowFix(func() {})}}
	}
	return ui.Pipe{State: ui.PipeOn, Level: a.flowLevel(&a.levels.cableSend)}
}

// cableLine is the cable itself: the mic and the sounds together on their
// way to CABLE Output, and what Resound does for the game meanwhile: hold
// push-to-talk.
func (a *App) cableLine(setupLater bool) ui.Pipe {
	switch {
	case a.cable.busy():
		return ui.Pipe{State: ui.PipeBroken, Badge: &ui.Badge{Icon: ui.IconAlert, Label: "Installing…", Tone: ui.ToneWarning,
			Tip: "VB-Cable is being installed. Please confirm the Windows prompts."}}
	case a.cable.state == cableRestart:
		return ui.Pipe{State: ui.PipeBroken, Badge: &ui.Badge{Icon: ui.IconAlert, Label: "Restart needed", Tone: ui.ToneWarning,
			Tip: "VB-Cable is installed. Restart your PC to finish; Resound reopens afterwards.", OnClick: a.flowFix(func() {})}}
	case !a.route.CableInstalled() && setupLater:
		return ui.Pipe{State: ui.PipeOff, Badge: &ui.Badge{Icon: ui.IconUnplugged, Label: "Next step",
			Tip: "Others hear you and your sounds through VB-Cable, a free virtual audio driver. The next step installs it."}}
	case !a.route.CableInstalled():
		return ui.Pipe{State: ui.PipeBroken, Badge: &ui.Badge{Icon: ui.IconAlert, Label: "VB-Cable missing", Tone: ui.ToneWarning,
			Tip:     "Others hear you and your sounds through VB-Cable, a free virtual audio driver by VB-Audio. Click to install it.",
			OnClick: a.flowFix(a.installCable)}}
	case a.ptt.pressed != nil:
		return ui.Pipe{State: ui.PipeOn, Level: a.flowLevel(&a.levels.others), Badge: &ui.Badge{
			Icon: ui.IconKeyboard, Label: "Holding " + a.ptt.pressed.String(), Tone: ui.ToneGood,
			Tip:     "Resound holds your game's push-to-talk key, so the game sends what plays. Click for push-to-talk settings.",
			OnClick: func() { a.openPage(pageKeys) }}}
	}
	return ui.Pipe{State: ui.PipeOn, Level: a.flowLevel(&a.levels.others)}
}
