package audio

import (
	"errors"
	"fmt"
	"testing"
)

func TestHRESULTErrorText(t *testing.T) {
	inner := errors.New("FormatMessage failed with: The system cannot find message text")
	for _, tc := range []struct {
		code uint32
		want string
	}{
		{0x8889000A, "another app is using this device exclusively (AUDCLNT_E_DEVICE_IN_USE)"},
		{0x88890004, "the device was unplugged or turned off (AUDCLNT_E_DEVICE_INVALIDATED)"},
		{0x88890099, "error 0x88890099"}, // unknown: the code, never go-ole's text
	} {
		err := fmt.Errorf("%q: Initialize: %w", "Speakers", &hresultError{code: tc.code, err: inner})
		if got, want := err.Error(), `"Speakers": Initialize: `+tc.want; got != want {
			t.Errorf("%#x: %q, want %q", tc.code, got, want)
		}
		if !errors.Is(err, inner) {
			t.Errorf("%#x: doesn't wrap the original error", tc.code)
		}
	}
}

func TestProblem(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{fmt.Errorf("monitor output: %w", &hresultError{code: 0x8889000A}), "another app is using this device exclusively"},
		{fmt.Errorf("cable output: GetCurrentPadding: %w", &hresultError{code: 0x88890004}), "the device was unplugged or turned off"},
		{fmt.Errorf("finding device: %w", ErrDeviceGone), "the device was unplugged or turned off"},
		{fmt.Errorf("mix format: %w (tag 0x2, 8 bits)", errUnsupportedFormat), "its audio format isn't supported"},
		{&hresultError{code: 0x88890099}, "the device couldn't be opened"},
		{errors.New("CreateEvent: something"), "the device couldn't be opened"},
	} {
		if got := Problem(tc.err); got != tc.want {
			t.Errorf("Problem(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
