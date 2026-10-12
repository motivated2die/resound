package vbcable

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pack.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractFlatPack(t *testing.T) {
	zp := writeZip(t, map[string]string{"VBCABLE_Setup_x64.exe": "exe", "readme.txt": "hi"})
	dest := filepath.Join(t.TempDir(), "pack")
	if err := extract(zp, dest); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "readme.txt")); err != nil || string(b) != "hi" {
		t.Fatalf("readme = %q, %v", b, err)
	}
}

func TestExtractRejectsPaths(t *testing.T) {
	for _, name := range []string{"../evil.exe", "sub/file.txt", `..\evil.exe`, "C:evil.exe"} {
		zp := writeZip(t, map[string]string{name: "x"})
		if err := extract(zp, filepath.Join(t.TempDir(), "pack")); err == nil {
			t.Errorf("%q: extracted, want an error", name)
		}
	}
}

func TestPendingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := LoadPending(dir); ok {
		t.Fatal("pending without a file")
	}
	boot := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	want := Pending{Started: boot.Add(time.Hour), BootTime: boot, ReturnTo: ReturnWizard, WizardStep: 3,
		Defaults: Defaults{Render: "{r}", Capture: "{c}"}}
	if err := SavePending(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadPending(dir)
	if !ok || !got.Started.Equal(want.Started) || got.ReturnTo != want.ReturnTo || got.WizardStep != 3 || got.Defaults != want.Defaults {
		t.Fatalf("got %+v, %v", got, ok)
	}
	if err := ClearPending(dir); err != nil {
		t.Fatal(err)
	}
	if err := ClearPending(dir); err != nil {
		t.Fatalf("second clear: %v", err)
	}
	if _, ok := LoadPending(dir); ok {
		t.Fatal("pending after clear")
	}
}

func TestRebooted(t *testing.T) {
	boot := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	p := Pending{Started: boot.Add(time.Hour), BootTime: boot}
	if p.Rebooted(boot.Add(3 * time.Second)) {
		t.Error("clock jitter counted as a restart")
	}
	if !p.Rebooted(boot.Add(2 * time.Hour)) {
		t.Error("restart not detected")
	}
}
