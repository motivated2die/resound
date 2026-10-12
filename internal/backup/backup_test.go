package backup

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 12, 13, 5, 9, 0, time.Local)

func write(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDefaultName(t *testing.T) {
	if got := DefaultName(t0, ""); got != "Resound-backup-2026-10-12_13-05-09.zip" {
		t.Fatal(got)
	}
	if got := DefaultName(t0, "before-restore"); got != "Resound-backup-2026-10-12_13-05-09-before-restore.zip" {
		t.Fatal(got)
	}
}

func TestCreateAndCleanRestore(t *testing.T) {
	data := t.TempDir()
	write(t, data, "config.json", `{"serverPort":8080}`)
	write(t, data, "library.json", `{"sounds":[1]}`)
	write(t, data, "debug.log", "not backed up")
	dest := filepath.Join(Dir(data), DefaultName(t0, ""))

	e, err := Create(data, dest, "0.6.0", t0)
	if err != nil {
		t.Fatal(err)
	}
	m, files, err := Read(dest)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0.6.0" || !m.Created.Equal(t0) || len(files) != 2 || e.Size == 0 {
		t.Fatalf("manifest %+v, %d files, entry %+v", m, len(files), e)
	}

	// Change everything, add a file the backup doesn't have, then restore.
	write(t, data, "config.json", `{"serverPort":9999}`)
	write(t, data, "library.json", `{}`)
	write(t, data, "remote.json", `{"pin":"123456"}`)
	if _, err := Restore(data, dest); err != nil {
		t.Fatal(err)
	}
	if got := read(t, data, "config.json"); got != `{"serverPort":8080}` {
		t.Errorf("config = %s", got)
	}
	if got := read(t, data, "library.json"); got != `{"sounds":[1]}` {
		t.Errorf("library = %s", got)
	}
	if _, err := os.Stat(filepath.Join(data, "remote.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("clean restore must remove files the backup doesn't contain")
	}
	if got := read(t, data, "debug.log"); got != "not backed up" {
		t.Error("other files must be left alone")
	}
}

func writeZipFile(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(data))
	}
	zw.Close()
	f.Close()
}

func TestReadRejects(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"app":"Resound","format":1,"version":"0.6.0"}`
	for name, files := range map[string]map[string]string{
		"no-manifest":  {"config.json": `{}`},
		"other-app":    {"manifest.json": `{"app":"Other","format":1}`},
		"traversal":    {"manifest.json": manifest, "../config.json": `{}`},
		"unknown-file": {"manifest.json": manifest, "evil.exe": `{}`},
		"bad-json":     {"manifest.json": manifest, "config.json": `{not json`},
		"newer-format": {"manifest.json": `{"app":"Resound","format":2,"version":"9.0"}`},
	} {
		p := filepath.Join(dir, name+".zip")
		writeZipFile(t, p, files)
		if _, _, err := Read(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p := filepath.Join(dir, "plain.zip")
	os.WriteFile(p, []byte("not a zip"), 0o644)
	if _, _, err := Read(p); !errors.Is(err, ErrNotBackup) {
		t.Errorf("plain file: %v", err)
	}
}

func TestRestoreRejectedChangesNothing(t *testing.T) {
	data := t.TempDir()
	write(t, data, "config.json", `{"a":1}`)
	p := filepath.Join(t.TempDir(), "bad.zip")
	writeZipFile(t, p, map[string]string{"manifest.json": `{"app":"Resound","format":1}`, "config.json": `{oops`})
	if _, err := Restore(data, p); err == nil {
		t.Fatal("restored a damaged backup")
	}
	if got := read(t, data, "config.json"); got != `{"a":1}` {
		t.Fatalf("config changed to %s", got)
	}
}

func TestListIndexAndFolder(t *testing.T) {
	data := t.TempDir()
	write(t, data, "config.json", `{}`)
	elsewhere := filepath.Join(t.TempDir(), "mine.zip")
	if _, err := Create(data, elsewhere, "0.6.0", t0); err != nil {
		t.Fatal(err)
	}
	inFolder := filepath.Join(Dir(data), DefaultName(t0.Add(time.Hour), ""))
	if _, err := Create(data, inFolder, "0.6.0", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// A backup copied into the folder by hand, not in the index.
	copied := filepath.Join(Dir(data), "copied.zip")
	src, _ := os.ReadFile(inFolder)
	os.WriteFile(copied, src, 0o644)

	list := List(data)
	if len(list) != 3 || list[len(list)-1].Path != elsewhere {
		t.Fatalf("list = %+v", list)
	}
	if !list[0].Created.After(list[2].Created) {
		t.Error("not newest first")
	}

	os.Remove(elsewhere) // gone: dropped from the list and the index
	if list = List(data); len(list) != 2 {
		t.Fatalf("after delete: %+v", list)
	}
	if idx := readIndex(data); len(idx) != 2 {
		t.Fatalf("index = %+v", idx)
	}
}
