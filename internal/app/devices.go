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
	a.setOutput(audio.RoleCable, a.cfg.CableDeviceID, a.cfg.CableDeviceID != "")
}

func (a *App) setOutput(role, id string, enabled bool) {
	start := time.Now()
	if err := a.engine.SetOutput(role, id, enabled); err != nil {
		logx.Printf("%v", err)
		a.playback.lastError = err.Error()
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

func (a *App) selectCable(id string) {
	a.cfg.CableDeviceID = id
	a.save()
	a.setOutput(audio.RoleCable, id, id != "")
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
	st, err := routing.Check(a.cfg.MicDeviceID)
	a.route, a.routeErr = st, err
	if err != nil {
		logx.Printf("routing check: %v", err)
		return
	}
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
	logx.Printf("routing: cable installed=%v, mic=%q, mic routed=%v", st.CableInstalled(), mic, st.MicRouted)
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

// useCableAsOutput selects VB-Cable's input as the "others hear" output.
func (a *App) useCableAsOutput() {
	if a.route.CableInput != nil {
		a.selectCable(a.route.CableInput.ID)
	}
}

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
