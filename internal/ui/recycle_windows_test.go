package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// RecycleFile removes the file from its folder (into the Recycle Bin).
func TestRecycleFile(t *testing.T) {
	if os.Getenv("RESOUND_RECYCLE_TEST") == "" {
		t.Skip("set RESOUND_RECYCLE_TEST=1: the test puts a file into this user's Recycle Bin")
	}
	path := filepath.Join(t.TempDir(), "Resound recycle test.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RecycleFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file is still there: %v", err)
	}
	if err := RecycleFile(path); err == nil {
		t.Error("recycling a missing file reported success")
	}
}
