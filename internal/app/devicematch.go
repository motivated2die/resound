package app

import (
	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/routing"
)

// A chosen device is saved by endpoint ID and by name. Some virtual
// devices get a new ID whenever Windows starts (Voicemeeter's do), so a
// saved ID that is gone is looked up again by its name.

// endpoint is a device as matching sees it.
type endpoint struct{ id, name string }

// matchDevice finds a saved device among devs. A known ID wins (and its
// current name is kept, in case it was renamed). A missing ID moves to the
// one device that has the saved name; with none, or several (which one was
// meant?), the choice stays as it was. changed reports a new ID or name.
func matchDevice(id, name string, devs []endpoint) (newID, newName string, changed bool) {
	if id == "" {
		return id, name, false
	}
	for _, d := range devs {
		if d.id == id {
			return id, d.name, d.name != name
		}
	}
	if name == "" {
		return id, name, false
	}
	match := ""
	for _, d := range devs {
		if d.name == name {
			if match != "" {
				return id, name, false
			}
			match = d.id
		}
	}
	if match == "" {
		return id, name, false
	}
	return match, name, true
}

// rematch applies matchDevice to one saved choice and logs a new ID.
// It reports whether the ID changed.
func rematch(what string, id, name *string, devs []endpoint) (idChanged, changed bool) {
	newID, newName, changed := matchDevice(*id, *name, devs)
	if !changed {
		return false, false
	}
	idChanged = newID != *id
	if idChanged {
		logx.Printf("devices: %s %q has a new ID, following it by its name: %s", what, newName, newID)
	}
	*id, *name = newID, newName
	return idChanged, true
}

func outputEndpoints(devs []audio.Device) []endpoint {
	out := make([]endpoint, len(devs))
	for i, d := range devs {
		out[i] = endpoint{d.ID, d.Name}
	}
	return out
}

func inputEndpoints(ins []routing.Input) []endpoint {
	out := make([]endpoint, len(ins))
	for i, in := range ins {
		out[i] = endpoint{in.ID, in.Name}
	}
	return out
}

// deviceName is the name of the device with id in devs, "" if none.
func deviceName(id string, devs []endpoint) string {
	for _, d := range devs {
		if d.id == id {
			return d.name
		}
	}
	return ""
}

// rematchOutputs follows the chosen outputs to their new IDs, after a
// fresh device list. Outputs already open move to the device found.
func (a *App) rematchOutputs() {
	devs := outputEndpoints(a.devices)
	monitorMoved, monitorChanged := rematch("monitor output", &a.cfg.MonitorDeviceID, &a.cfg.MonitorDeviceName, devs)
	cableMoved, cableChanged := rematch("cable output", &a.cfg.CableDeviceID, &a.cfg.CableDeviceName, devs)
	if monitorChanged || cableChanged {
		a.save()
	}
	if !a.outputsOpen {
		return // openOutputs opens the new IDs
	}
	if monitorMoved {
		a.setOutput(audio.RoleMonitor, a.cfg.MonitorDeviceID, true)
	}
	if cableMoved {
		a.openCableOutput()
	}
}

// rematchInputs follows the chosen recording devices to their new IDs,
// after a fresh routing check. It reports whether an ID changed, so the
// check must run again with it.
func (a *App) rematchInputs() bool {
	devs := inputEndpoints(a.route.Inputs)
	micMoved, micChanged := rematch("microphone", &a.cfg.MicDeviceID, &a.cfg.MicDeviceName, devs)
	appsMoved, appsChanged := rematch("apps' microphone", &a.cfg.AppsMicDeviceID, &a.cfg.AppsMicDeviceName, devs)
	if micChanged || appsChanged {
		a.save()
	}
	return micMoved || appsMoved
}
