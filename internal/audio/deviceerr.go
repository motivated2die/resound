package audio

import (
	"errors"
	"fmt"
)

// Errors from opening and running an output. WASAPI fails with HRESULTs
// (AUDCLNT_E_*) that have no message text in Windows, so go-ole's error
// text for them is FormatMessage's own failure ("The system cannot find
// message text for message number 0x%1…"). hresultError names them
// instead, and Problem turns any output error into a short sentence for
// the panel.

// ErrDeviceGone means the chosen device isn't there: unplugged, disabled,
// or removed from Windows.
var ErrDeviceGone = errors.New("device not found or not active")

// errUnsupportedFormat wraps mix formats the mixer can't write.
var errUnsupportedFormat = errors.New("unsupported mix format")

// hresultCause is what a known HRESULT means: its SDK name for the log and
// a short explanation for the user.
type hresultCause struct{ name, problem string }

var hresultCauses = map[uint32]hresultCause{
	0x88890004: {"AUDCLNT_E_DEVICE_INVALIDATED", "the device was unplugged or turned off"},
	0x88890008: {"AUDCLNT_E_UNSUPPORTED_FORMAT", "its audio format isn't supported"},
	// In shared mode (Resound's), "in use" means another app holds the
	// device in exclusive mode: e.g. a mixer such as Voicemeeter or Sonar
	// that took the hardware for itself.
	0x8889000A: {"AUDCLNT_E_DEVICE_IN_USE", "another app is using this device exclusively"},
	0x8889000F: {"AUDCLNT_E_ENDPOINT_CREATE_FAILED", "Windows couldn't open the device"},
	0x88890010: {"AUDCLNT_E_SERVICE_NOT_RUNNING", "Windows' audio service isn't running"},
	0x88890012: {"AUDCLNT_E_EXCLUSIVE_MODE_ONLY", "the device only works in exclusive mode"},
	0x88890026: {"AUDCLNT_E_RESOURCES_INVALIDATED", "the device's settings changed"},
	0x80070005: {"E_ACCESSDENIED", "access to the device was denied"},
}

// hresultError is a failed COM call, named by its HRESULT. It wraps the
// original error (go-ole's OleError), but never prints its text.
type hresultError struct {
	code uint32
	err  error
}

func (e *hresultError) Error() string {
	if c, ok := hresultCauses[e.code]; ok {
		return fmt.Sprintf("%s (%s)", c.problem, c.name)
	}
	return fmt.Sprintf("error %#08x", e.code)
}

func (e *hresultError) Unwrap() error { return e.err }

// Problem says in a few words why an output can't play, for the panel,
// e.g. "another app is using this device exclusively". Errors without a
// known cause get "the device couldn't be opened". nil gives "".
func Problem(err error) string {
	var he *hresultError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &he):
		if c, ok := hresultCauses[he.code]; ok {
			return c.problem
		}
	case errors.Is(err, ErrDeviceGone):
		return "the device was unplugged or turned off"
	case errors.Is(err, errUnsupportedFormat):
		return "its audio format isn't supported"
	}
	return "the device couldn't be opened"
}
