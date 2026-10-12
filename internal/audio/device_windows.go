package audio

import (
	"strings"

	"github.com/motivated2die/resound/internal/wincom"
)

// Device is an active render endpoint.
type Device struct {
	ID      string // WASAPI endpoint ID, stable across renames
	Name    string // friendly name, e.g. "CABLE Input (VB-Audio Virtual Cable)"
	Default bool   // current default console output
}

// IsVirtualCable reports whether the device looks like a VB-Audio cable input.
func (d Device) IsVirtualCable() bool {
	n := strings.ToLower(d.Name)
	return strings.Contains(n, "cable input") || strings.Contains(n, "vb-audio")
}

// ListOutputDevices returns all active render endpoints.
func ListOutputDevices() ([]Device, error) {
	var out []Device
	err := wincom.Run(func() error {
		e, err := wincom.NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		eps, err := wincom.Enumerate(e, wincom.Render)
		for _, ep := range eps {
			out = append(out, Device{ID: ep.ID, Name: ep.Name, Default: ep.Default})
		}
		return err
	})
	return out, err
}
