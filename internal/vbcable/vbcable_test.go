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

func TestClassify(t *testing.T) {
	up := Driver{Present: true, Instance: `ROOT\MEDIA\0003`}
	waiting := Driver{Present: true, Instance: `ROOT\MEDIA\0003`, Problem: 14}
	for _, c := range []struct {
		name      string
		driver    Driver
		endpoints bool
		want      Health
	}{
		{"nothing there", Driver{}, false, HealthMissing},
		{"working", up, true, HealthReady},
		{"waiting for a restart, no devices yet", waiting, false, HealthPending},
		{"problem although the devices show", waiting, true, HealthPending},
		{"driver up, devices not yet", up, false, HealthPending},
		{"just uninstalled, devices linger", Driver{}, true, HealthRemoved},
	} {
		if got := Classify(c.driver, c.endpoints); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNewestPack(t *testing.T) {
	link := func(n string) string {
		return `<a href="https://download.vb-audio.com/Download_CABLE/VBCABLE_Driver_Pack` + n + `.zip">`
	}
	for _, c := range []struct{ name, page, want string }{
		{"no links", "<html></html>", ""},
		{"only older and known", link("43") + link("45"), ""},
		{"a newer one, whatever the order", link("46") + link("45") + link("47") + link("9"),
			"https://download.vb-audio.com/Download_CABLE/VBCABLE_Driver_Pack47.zip"},
		{"another host is no link", `https://evil.example/Download_CABLE/VBCABLE_Driver_Pack99.zip`, ""},
		{"another folder is no link", `https://download.vb-audio.com/x/VBCABLE_Driver_Pack99.zip`, ""},
	} {
		if got := newestPack(c.page); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
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
