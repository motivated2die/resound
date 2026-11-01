package wincom

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestFormatWithRate(t *testing.T) {
	// 44.1 kHz, stereo, 24 bits in a WAVEFORMATEXTENSIBLE
	f := make([]byte, waveFormatHeader+22)
	binary.LittleEndian.PutUint16(f, waveFormatExtensible)
	binary.LittleEndian.PutUint16(f[2:], 2)
	binary.LittleEndian.PutUint32(f[4:], 44100)
	binary.LittleEndian.PutUint32(f[8:], 44100*6)
	binary.LittleEndian.PutUint16(f[12:], 6)
	binary.LittleEndian.PutUint16(f[14:], 24)
	binary.LittleEndian.PutUint16(f[16:], 22)
	f[20] = 0xAB // part of the extension: kept

	got := formatWithRate(f, 48000)
	if formatRate(got) != 48000 {
		t.Errorf("rate %d", formatRate(got))
	}
	if avg := binary.LittleEndian.Uint32(got[8:]); avg != 48000*6 {
		t.Errorf("bytes per second %d", avg)
	}
	if got[2] != 2 || got[12] != 6 || got[14] != 24 || got[20] != 0xAB {
		t.Errorf("other fields changed: % x", got)
	}
	if formatRate(f) != 44100 {
		t.Error("the original changed")
	}
}

// Reading needs no rights and changes nothing: VB-Cable's rates when it is
// installed, any other device's otherwise.
func TestDeviceRate(t *testing.T) {
	err := Run(func() error {
		e, err := NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		for _, flow := range []uint32{Render, Capture} {
			eps, err := Enumerate(e, flow)
			if err != nil {
				return err
			}
			for _, ep := range eps {
				rate, err := DeviceRate(ep.ID)
				if err != nil {
					t.Errorf("%s: %v", ep.Name, err)
					continue
				}
				t.Logf("%s: %d Hz", ep.Name, rate)
				if rate < 8000 || rate > 768000 {
					t.Errorf("%s: odd rate %d", ep.Name, rate)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Skipf("no audio: %v", err)
	}
}

// TestSetDeviceRateLive moves CABLE Output to 44.1 kHz and back, so apps using
// the cable reconnect (opt-in: RESOUND_LIVE_CABLE_RATE=1).
func TestSetDeviceRateLive(t *testing.T) {
	if os.Getenv("RESOUND_LIVE_CABLE_RATE") != "1" {
		t.Skip("RESOUND_LIVE_CABLE_RATE not set")
	}
	err := Run(func() error {
		e, err := NewEnumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		eps, err := Enumerate(e, Capture)
		if err != nil {
			return err
		}
		for _, ep := range eps {
			if !strings.HasPrefix(strings.ToLower(ep.Name), "cable output") {
				continue
			}
			before, err := DeviceRate(ep.ID)
			if err != nil {
				return err
			}
			other := 44100
			if before == other {
				other = 48000
			}
			defer func() {
				if err := SetDeviceRate(ep.ID, before); err != nil {
					t.Errorf("putting %d Hz back: %v", before, err)
				}
			}()
			return SetDeviceRate(ep.ID, other)
		}
		return errors.New("no VB-Cable")
	})
	if err != nil {
		t.Fatal(err)
	}
}
