package app

import (
	"testing"

	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/routing"
)

func TestMatchDevice(t *testing.T) {
	const vm = "Voicemeeter Input (VB-Audio Voicemeeter VAIO)"
	devs := []endpoint{
		{"{new-vm}", vm},
		{"{hyperx}", "Headphones (HyperX Cloud Alpha Wireless)"},
		{"{usb-1}", "Speakers (USB Audio)"},
		{"{usb-2}", "Speakers (USB Audio)"},
	}
	for _, c := range []struct {
		name, id, saved  string
		wantID, wantName string
		changed          bool
	}{
		{"nothing chosen", "", "", "", "", false},
		{"ID still there", "{hyperx}", "Headphones (HyperX Cloud Alpha Wireless)", "{hyperx}", "Headphones (HyperX Cloud Alpha Wireless)", false},
		{"name filled in for a config from before names", "{hyperx}", "", "{hyperx}", "Headphones (HyperX Cloud Alpha Wireless)", true},
		{"renamed in Windows: the ID wins", "{hyperx}", "Old name", "{hyperx}", "Headphones (HyperX Cloud Alpha Wireless)", true},
		{"new ID after a restart: found by name", "{old-vm}", vm, "{new-vm}", vm, true},
		{"gone, no name saved", "{old-vm}", "", "{old-vm}", "", false},
		{"gone, name not there either", "{old}", "Speakers (Realtek(R) Audio)", "{old}", "Speakers (Realtek(R) Audio)", false},
		{"gone, name ambiguous", "{old}", "Speakers (USB Audio)", "{old}", "Speakers (USB Audio)", false},
	} {
		id, name, changed := matchDevice(c.id, c.saved, devs)
		if id != c.wantID || name != c.wantName || changed != c.changed {
			t.Errorf("%s: got %q %q %v, want %q %q %v", c.name, id, name, changed, c.wantID, c.wantName, c.changed)
		}
	}
}

// The chosen microphones follow their names to new IDs, as Voicemeeter's
// recording devices get after a Windows restart.
func TestRematchInputs(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir()) // save() writes the config
	const b1 = "Voicemeeter Out B1 (VB-Audio Voicemeeter VAIO)"
	const mic = "Microphone (4- HyperX QuadCast S)"
	a := &App{
		cfg: config.Config{MicDeviceID: "{mic}", MicDeviceName: mic, AppsMicDeviceID: "{old-b1}", AppsMicDeviceName: b1},
		route: routing.Status{Inputs: []routing.Input{
			{ID: "{mic}", Name: mic},
			{ID: "{new-b1}", Name: b1},
		}},
	}
	if !a.rematchInputs() {
		t.Fatal("rematchInputs = false, want true (the apps' mic moved)")
	}
	if a.cfg.AppsMicDeviceID != "{new-b1}" || a.cfg.MicDeviceID != "{mic}" {
		t.Errorf("got mic %q, apps' mic %q", a.cfg.MicDeviceID, a.cfg.AppsMicDeviceID)
	}
	if saved := config.Load(); saved.AppsMicDeviceID != "{new-b1}" {
		t.Errorf("saved apps' mic %q, want the new ID", saved.AppsMicDeviceID)
	}
	if a.rematchInputs() {
		t.Error("second rematchInputs = true, want false (nothing left to follow)")
	}
}
