package audio

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// CheckFile accepts playable sounds, also ones shorter than the half second
// it decodes, and refuses what isn't one, such as a web page saved as .mp3.
func TestCheckFile(t *testing.T) {
	for _, d := range []time.Duration{time.Second, 20 * time.Millisecond} {
		if err := CheckFile(writeTestWAV(t, t.TempDir(), d)); err != nil {
			t.Errorf("%v WAV: %v", d, err)
		}
	}
	page := filepath.Join(t.TempDir(), "page.mp3")
	if err := os.WriteFile(page, []byte("<!DOCTYPE html><html><body>Not found</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckFile(page); !errors.Is(err, ErrUnreadable) {
		t.Errorf("HTML as .mp3: err = %v, want ErrUnreadable", err)
	}
	if err := CheckFile(filepath.Join(t.TempDir(), "missing.ogg")); !errors.Is(err, ErrUnreadable) {
		t.Errorf("missing file: err = %v, want ErrUnreadable", err)
	}
}
