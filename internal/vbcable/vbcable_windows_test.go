package vbcable

import (
	"os"
	"testing"

	"github.com/motivated2die/resound/internal/wincom"
)

// TestVerifySigner checks a real VB-CABLE setup, when one is given:
// VBCABLE_SETUP=<path to VBCABLE_Setup_x64.exe> go test ./internal/vbcable
func TestVerifySigner(t *testing.T) {
	setup := os.Getenv("VBCABLE_SETUP")
	if setup == "" {
		t.Skip("VBCABLE_SETUP not set")
	}
	name, err := verifySigner(setup)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("signer: %s", name)
}

func TestVerifySignerRejectsOthers(t *testing.T) {
	// notepad.exe is signed by Microsoft (through a catalog, or embedded):
	// either way it must not pass as VB-Audio.
	if _, err := verifySigner(os.Getenv("SystemRoot") + `\System32\notepad.exe`); err == nil {
		t.Fatal("notepad.exe accepted as VB-Audio")
	}
	exe, err := os.Executable() // the unsigned test binary
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifySigner(exe); err == nil {
		t.Fatal("unsigned file accepted")
	}
}

// TestDownload fetches the real pack (no install): VBCABLE_DOWNLOAD=1.
func TestDownload(t *testing.T) {
	if os.Getenv("VBCABLE_DOWNLOAD") != "1" {
		t.Skip("VBCABLE_DOWNLOAD not set")
	}
	var last int64
	setup, err := Download(t.Context(), t.TempDir(), func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("setup %s, %d bytes downloaded", setup, last)
}

// Restoring an unchanged snapshot never changes a default device.
func TestRestoreUnchangedDefaults(t *testing.T) {
	d, err := SnapshotDefaults()
	if err != nil {
		t.Skipf("no audio devices: %v", err)
	}
	changed, err := RestoreDefaults(d)
	if err != nil || len(changed) > 0 {
		t.Fatalf("changed %v, %v", changed, err)
	}
	t.Logf("defaults: %+v", d)
}

// Setting the current default again checks the IPolicyConfig call without
// changing anything.
func TestSetDefaultToCurrent(t *testing.T) {
	d, err := SnapshotDefaults()
	if err != nil || d.Render == "" {
		t.Skipf("no default playback device: %v", err)
	}
	if err := wincom.Run(func() error { return wincom.SetDefaultEndpoint(d.Render, wincom.RoleConsole) }); err != nil {
		t.Fatal(err)
	}
	if after, _ := SnapshotDefaults(); after.Render != d.Render {
		t.Fatalf("default changed to %s", after.Render)
	}
}
