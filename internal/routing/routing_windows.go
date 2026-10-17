package routing

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"github.com/moutend/go-wca/pkg/wca"

	"github.com/motivated2die/resound/internal/wincom"
)

// Input is an active capture endpoint (microphone, virtual input, ...).
type Input struct {
	ID      string
	Name    string
	Default bool
	Muted   bool
	// Listening is "Listen to this device"; ListenTarget is the render
	// endpoint ID it plays through ("" = default playback device).
	Listening    bool
	ListenTarget string
}

// IsCableOutput reports whether this is VB-Cable's "CABLE Output" (the
// virtual microphone others hear), which must never be routed into itself.
func (in Input) IsCableOutput() bool { return IsCableName(in.Name) }

// IsVoicemeeter reports whether this is one of Voicemeeter's virtual
// devices (e.g. "Voicemeeter Out B1"): a mixer's output, never a real mic.
func (in Input) IsVoicemeeter() bool { return IsVoicemeeterName(in.Name) }

// Endpoint is a render endpoint relevant to routing.
type Endpoint struct {
	ID   string
	Name string
}

// Status is the result of Check.
type Status struct {
	CableInput  *Endpoint // render side: where Resound plays "others hear" audio
	CableOutput *Input    // capture side: the mic to select in Discord/games
	Inputs      []Input   // all capture devices, cable output included
	Mic         *Input    // the chosen (or automatically picked) real microphone
	// MicRouted is true when Mic listens through CableInput.
	MicRouted bool
	// Voicemeeter is true when Voicemeeter's virtual devices are present,
	// i.e. the mixer is installed. Its own settings are never read.
	Voicemeeter bool
}

// CableInstalled reports whether both ends of VB-Cable were found.
func (s Status) CableInstalled() bool { return s.CableInput != nil && s.CableOutput != nil }

// IsCableName matches VB-Cable's main pair ("CABLE Input"/"CABLE Output",
// "(VB-Audio Virtual Cable)"), not the 16-channel or A/B variants.
func IsCableName(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "cable input") || strings.HasPrefix(n, "cable output")
}

// IsVoicemeeterName matches the devices of Voicemeeter's virtual audio
// driver ("Voicemeeter Input", "Voicemeeter Out B1", … "(VB-Audio
// Voicemeeter VAIO)").
func IsVoicemeeterName(name string) bool {
	return strings.Contains(strings.ToLower(name), "voicemeeter")
}

// Check inspects the current device setup. micID selects the microphone;
// "" picks the default recording device unless that is the cable itself, a
// Voicemeeter device or appsMicID (the device others' apps use as their
// microphone with the user's own routing).
func Check(micID, appsMicID string) (Status, error) {
	var st Status
	err := wincom.Run(func() error {
		e, err := wincom.NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()

		outs, err := wincom.Enumerate(e, wincom.Render)
		if err != nil {
			return err
		}
		for _, o := range outs {
			if st.CableInput == nil && strings.HasPrefix(strings.ToLower(o.Name), "cable input") {
				st.CableInput = &Endpoint{ID: o.ID, Name: o.Name}
			}
			if IsVoicemeeterName(o.Name) {
				st.Voicemeeter = true
			}
		}

		ins, err := wincom.Enumerate(e, wincom.Capture)
		if err != nil {
			return err
		}
		for _, ep := range ins {
			in := Input{ID: ep.ID, Name: ep.Name, Default: ep.Default}
			if dev, err := wincom.Find(e, wincom.Capture, ep.ID); err == nil {
				readInputState(dev, &in)
				dev.Release()
			}
			st.Inputs = append(st.Inputs, in)
		}
		return nil
	})
	if err != nil {
		return st, err
	}

	for i := range st.Inputs {
		if st.CableOutput == nil && strings.HasPrefix(strings.ToLower(st.Inputs[i].Name), "cable output") {
			st.CableOutput = &st.Inputs[i]
		}
		if st.Inputs[i].IsVoicemeeter() {
			st.Voicemeeter = true
		}
	}
	st.Mic = pickMic(st.Inputs, micID, appsMicID)
	if st.Mic != nil && st.CableInput != nil {
		st.MicRouted = st.Mic.Listening && strings.EqualFold(st.Mic.ListenTarget, st.CableInput.ID)
	}
	return st, nil
}

func pickMic(inputs []Input, micID, appsMicID string) *Input {
	if micID != "" {
		for i := range inputs {
			if inputs[i].ID == micID {
				return &inputs[i]
			}
		}
		return nil // chosen mic is unplugged
	}
	// Never a mixer's output: muting it while sounds play would silence
	// the sounds for others too.
	real := func(in Input) bool {
		return !in.IsCableOutput() && !in.IsVoicemeeter() && (appsMicID == "" || in.ID != appsMicID)
	}
	for i := range inputs {
		if inputs[i].Default && real(inputs[i]) {
			return &inputs[i]
		}
	}
	for i := range inputs {
		if real(inputs[i]) {
			return &inputs[i]
		}
	}
	return nil
}

func readInputState(dev *wca.IMMDevice, in *Input) {
	var ps *wca.IPropertyStore
	if err := dev.OpenPropertyStore(wca.STGM_READ, &ps); err == nil {
		in.Listening = readBool(ps, pkeyListen)
		in.ListenTarget = readString(ps, pkeyListenTarget)
		ps.Release()
	}
	var vol *wca.IAudioEndpointVolume
	if err := wincom.Activate(dev, wincom.IID_IAudioEndpointVolume, unsafe.Pointer(&vol)); err == nil {
		if muted, err := wincom.GetMute(vol); err == nil {
			in.Muted = muted
		}
		vol.Release()
	}
}

// SetMute mutes or unmutes a capture device. No admin rights needed.
func SetMute(inputID string, mute bool) error {
	return wincom.Run(func() error {
		e, err := wincom.NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		dev, err := wincom.Find(e, wincom.Capture, inputID)
		if err != nil {
			return err
		}
		defer dev.Release()
		var vol *wca.IAudioEndpointVolume
		if err := wincom.Activate(dev, wincom.IID_IAudioEndpointVolume, unsafe.Pointer(&vol)); err != nil {
			return fmt.Errorf("endpoint volume: %w", err)
		}
		defer vol.Release()
		return vol.SetMute(mute, nil)
	})
}

// IsMuted reports a capture device's mute state.
func IsMuted(inputID string) (bool, error) {
	var muted bool
	err := wincom.Run(func() error {
		e, err := wincom.NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		dev, err := wincom.Find(e, wincom.Capture, inputID)
		if err != nil {
			return err
		}
		defer dev.Release()
		var vol *wca.IAudioEndpointVolume
		if err := wincom.Activate(dev, wincom.IID_IAudioEndpointVolume, unsafe.Pointer(&vol)); err != nil {
			return err
		}
		defer vol.Release()
		muted, err = wincom.GetMute(vol)
		return err
	})
	return muted, err
}

// ErrAccessDenied means the property store could not be opened for writing:
// the process is not elevated.
var ErrAccessDenied = errors.New("access denied: administrator rights are required")

// apply sets "Listen to this device" on inputID. When enable is true, the
// device plays through targetID. It must run elevated.
func apply(inputID, targetID string, enable bool) error {
	return wincom.Run(func() error {
		e, err := wincom.NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		dev, err := wincom.Find(e, wincom.Capture, inputID)
		if err != nil {
			return fmt.Errorf("microphone: %w", err)
		}
		defer dev.Release()

		var ps *wca.IPropertyStore
		if err := dev.OpenPropertyStore(wca.STGM_READ_WRITE, &ps); err != nil {
			if wincom.IsAccessDenied(err) {
				return ErrAccessDenied
			}
			return fmt.Errorf("opening property store: %w", err)
		}
		defer ps.Release()

		if enable {
			if err := writeString(ps, pkeyListenTarget, targetID); err != nil {
				return fmt.Errorf("writing playback target: %w", err)
			}
		}
		if err := writeBool(ps, pkeyListen, enable); err != nil {
			return fmt.Errorf("writing listen flag: %w", err)
		}
		_ = commit(ps)

		// Verify by reading back through the same store.
		if got := readBool(ps, pkeyListen); got != enable {
			return fmt.Errorf("verification failed: listen flag is %v", got)
		}
		if enable {
			if got := readString(ps, pkeyListenTarget); !strings.EqualFold(got, targetID) {
				return fmt.Errorf("verification failed: playback target is %q", got)
			}
		}
		return nil
	})
}
