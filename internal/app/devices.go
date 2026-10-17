package app

import (
	"errors"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
)

// refreshDevices re-reads the output list; verbose logs every device.
func (a *App) refreshDevices(verbose bool) {
	devs, err := audio.ListOutputDevices()
	a.devices, a.devErr = devs, err
	if err != nil {
		logx.Printf("listing outputs: %v", err)
		return
	}
	if verbose {
		for _, d := range devs {
			logx.Printf("output: %q default=%v id=%s", d.Name, d.Default, d.ID)
		}
	}
}

func (a *App) openOutputs() {
	a.setOutput(audio.RoleMonitor, a.cfg.MonitorDeviceID, true)
	a.openCableOutput()
	a.outputsOpen = true
}

// cableOutputID is the device "others hear" plays into: the chosen one, or
// automatically VB-Cable's CABLE Input. "" = none (no cable installed).
func (a *App) cableOutputID() string {
	if a.cfg.CableDeviceID != "" {
		return a.cfg.CableDeviceID
	}
	if a.route.CableInput != nil {
		return a.route.CableInput.ID
	}
	return ""
}

func (a *App) openCableOutput() {
	id := a.cableOutputID()
	a.cableOpenID = id
	a.setOutput(audio.RoleCable, id, id != "")
}

// syncCableOutput opens the automatic cable output once VB-Cable appears
// (after its install, or plugged in later) and closes it when it goes.
func (a *App) syncCableOutput() {
	if a.outputsOpen && a.cableOutputID() != a.cableOpenID {
		a.openCableOutput()
	}
}

func (a *App) setOutput(role, id string, enabled bool) {
	start := time.Now()
	// A failure shows under the flow while it lasts (outputErrorRows, from
	// the engine's state). A test play that failed on the old device is
	// outdated now.
	a.playback.lastError = ""
	if err := a.engine.SetOutput(role, id, enabled); err != nil {
		logx.Printf("%v", err)
	}
	for _, s := range a.engine.Status() {
		if s.Role == role && s.Running {
			logx.Printf("%s output open in %v: %q, %s, buffer %v (%d frames), stream latency %v",
				role, time.Since(start).Round(time.Millisecond), s.Info.DeviceName, s.FormatText(),
				s.Info.Buffer.Round(100*time.Microsecond), s.Info.BufferFrames, s.Info.StreamLatency)
		}
	}
}

func (a *App) selectMonitor(id string) {
	a.cfg.MonitorDeviceID = id
	a.save()
	a.setOutput(audio.RoleMonitor, id, true)
	a.render()
}

// selectCable chooses the "others hear" output: "" = automatic (VB-Cable),
// which also ends an own routing.
func (a *App) selectCable(id string) {
	a.cfg.CableDeviceID = id
	if id == "" && a.cfg.AppsMicDeviceID != "" {
		a.cfg.AppsMicDeviceID = ""
		a.refreshRouting(true) // the automatic mic may have skipped it
	}
	a.save()
	a.openCableOutput()
	a.render()
}

// ownRouting reports whether sounds for others play into a device of the
// user's own routing (e.g. a Voicemeeter input) instead of VB-Cable. The
// user's mixer then combines the mic and the sounds; Resound doesn't route
// the mic, and never reads or changes the mixer's settings.
func (a *App) ownRouting() bool {
	return a.cfg.CableDeviceID != "" && !a.cableSelected()
}

// appsMic is the recording device others' apps use as their microphone
// with an own routing; nil while none is chosen or it is missing.
func (a *App) appsMic() *routing.Input {
	if a.cfg.AppsMicDeviceID == "" {
		return nil
	}
	for i := range a.route.Inputs {
		if a.route.Inputs[i].ID == a.cfg.AppsMicDeviceID {
			return &a.route.Inputs[i]
		}
	}
	return nil
}

// selectAppsMic chooses the device others' apps listen to with an own
// routing; the flow shows it and measures what arrives there.
func (a *App) selectAppsMic(id string) {
	a.cfg.AppsMicDeviceID = id
	a.save()
	a.refreshRouting(true)
	a.render()
}

func (a *App) restartOutputs() {
	a.refreshDevices(true)
	a.openOutputs()
	a.render()
}

// --- routing ---------------------------------------------------------------

// refreshRouting re-reads cable, microphone and listen state.
func (a *App) refreshRouting(verbose bool) {
	st, err := routing.Check(a.cfg.MicDeviceID, a.cfg.AppsMicDeviceID)
	a.route, a.routeErr = st, err
	if err != nil {
		logx.Printf("routing check: %v", err)
		return
	}
	a.syncCableOutput()
	if !verbose {
		return
	}
	for _, in := range st.Inputs {
		logx.Printf("input: %q default=%v muted=%v listen=%v target=%q id=%s",
			in.Name, in.Default, in.Muted, in.Listening, in.ListenTarget, in.ID)
	}
	mic := "(none)"
	if st.Mic != nil {
		mic = st.Mic.Name
	}
	logx.Printf("routing: cable installed=%v, voicemeeter=%v, mic=%q, mic routed=%v, own routing=%v",
		st.CableInstalled(), st.Voicemeeter, mic, st.MicRouted, a.ownRouting())
}

func (a *App) selectMic(id string) {
	if a.playback.mutedMic != "" {
		a.releaseMic() // don't leave the old mic muted
	}
	a.cfg.MicDeviceID = id
	a.save()
	a.refreshRouting(true)
	a.render()
}

func (a *App) toggleMuteWhilePlaying() {
	a.cfg.MuteMicWhilePlaying = !a.cfg.MuteMicWhilePlaying
	if !a.cfg.MuteMicWhilePlaying {
		a.releaseMic()
	}
	a.save()
	a.render()
}

// useCableAsOutput goes back to the automatic "others hear" output,
// VB-Cable's CABLE Input.
func (a *App) useCableAsOutput() { a.selectCable("") }

// setMicRouting switches "Listen to this device" on the microphone through
// the elevated helper. The UAC prompt can take a while, so it runs off the
// UI thread and reports back via Post.
func (a *App) setMicRouting(enable bool) {
	if a.busy != "" || a.route.Mic == nil || (enable && a.route.CableInput == nil) {
		return
	}
	micID, target := a.route.Mic.ID, ""
	if a.route.CableInput != nil {
		target = a.route.CableInput.ID
	}
	a.busy = "Waiting for administrator approval…"
	a.routeErr = nil
	a.render()
	go func() {
		err := routing.ApplyElevated(micID, target, enable)
		a.tray.Post(func() {
			a.busy = ""
			switch {
			case errors.Is(err, routing.ErrCancelled):
				logx.Printf("routing: UAC prompt declined")
			case err != nil:
				logx.Printf("routing: %v", err)
				a.routeErr = err
			default:
				logx.Printf("routing: mic listening=%v via helper", enable)
			}
			if err == nil || errors.Is(err, routing.ErrCancelled) {
				a.refreshRouting(true)
			}
			a.render()
		})
	}()
}
