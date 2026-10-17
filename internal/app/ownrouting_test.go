package app

import (
	"testing"

	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/routing"
)

func TestOwnRouting(t *testing.T) {
	cable := &routing.Endpoint{ID: "cable-in", Name: "CABLE Input (VB-Audio Virtual Cable)"}
	b1 := routing.Input{ID: "b1", Name: "Voicemeeter Out B1 (VB-Audio Voicemeeter VAIO)"}
	mic := routing.Input{ID: "mic", Name: "Mikrofon (BlackShark V3 Pro XBOX - Chat)"}
	for _, c := range []struct {
		name         string
		cfg          config.Config
		route        routing.Status
		own, ready   bool
		appsMicFound bool
	}{
		{"VB-Cable missing", config.Config{}, routing.Status{Inputs: []routing.Input{mic}}, false, false, false},
		{"own routing, apps' mic not chosen",
			config.Config{CableDeviceID: "aux"}, routing.Status{Inputs: []routing.Input{mic, b1}}, true, false, false},
		{"own routing, ready without VB-Cable",
			config.Config{CableDeviceID: "aux", AppsMicDeviceID: "b1"}, routing.Status{Inputs: []routing.Input{mic, b1}}, true, true, true},
		{"own routing, apps' mic gone (mixer uninstalled)",
			config.Config{CableDeviceID: "aux", AppsMicDeviceID: "b1"}, routing.Status{Inputs: []routing.Input{mic}}, true, false, false},
		{"own routing next to an installed VB-Cable",
			config.Config{CableDeviceID: "aux", AppsMicDeviceID: "b1"},
			routing.Status{CableInput: cable, Inputs: []routing.Input{mic, b1}}, true, true, true},
		{"VB-Cable chosen: a leftover apps' mic doesn't count",
			config.Config{AppsMicDeviceID: "b1"},
			routing.Status{CableInput: cable, Inputs: []routing.Input{mic, b1}}, false, false, true},
	} {
		a := &App{cfg: c.cfg, route: c.route}
		if got := a.ownRouting(); got != c.own {
			t.Errorf("%s: ownRouting = %v, want %v", c.name, got, c.own)
		}
		if got := a.voiceReady(); got != c.ready {
			t.Errorf("%s: voiceReady = %v, want %v", c.name, got, c.ready)
		}
		if got := a.appsMic() != nil; got != c.appsMicFound {
			t.Errorf("%s: appsMic found = %v, want %v", c.name, got, c.appsMicFound)
		}
	}
}

func TestShortDeviceName(t *testing.T) {
	for in, want := range map[string]string{
		"Voicemeeter Out B1 (VB-Audio Voicemeeter VAIO)": "Voicemeeter Out B1",
		"Mikrofon (BlackShark V3 Pro XBOX - Chat)":       "Mikrofon",
		"Realtek(R) Audio": "Realtek(R) Audio",
		"My mic":           "My mic",
	} {
		if got := shortDeviceName(in); got != want {
			t.Errorf("shortDeviceName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cardDeviceName("Voicemeeter Out B1 (VB-Audio Voicemeeter VAIO)"); got != "Out B1" {
		t.Errorf("cardDeviceName = %q, want Out B1", got)
	}
	if got := cardDeviceName("CABLE Output (VB-Audio Virtual Cable)"); got != "CABLE Output" {
		t.Errorf("cardDeviceName = %q, want CABLE Output", got)
	}
}
