package audio

import (
	"fmt"
	"testing"

	"github.com/go-ole/go-ole"
)

// TestDescribeHRESULT checks go-ole's error for a WASAPI code: its own text
// is FormatMessage failing, which must not reach the log or the panel, and
// describing twice (a step's error, then the output failing) adds nothing.
func TestDescribeHRESULT(t *testing.T) {
	raw := ole.NewError(0x8889000A)
	err := fmt.Errorf("GetCurrentPadding: %w", describeHRESULT(raw))
	err = describeHRESULT(err)
	if got, want := err.Error(), "GetCurrentPadding: another app is using this device exclusively (AUDCLNT_E_DEVICE_IN_USE)"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	if plain := fmt.Errorf("x"); describeHRESULT(plain) != plain {
		t.Fatal("non-COM errors must pass unchanged")
	}
}
