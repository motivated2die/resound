package backup

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSoundsName(t *testing.T) {
	if got := SoundsName(t0); got != "Resound-sounds-2026-10-12_13-05-09.zip" {
		t.Fatal(got)
	}
}

// soundTree builds two library folders that are both named "Sounds": one
// with a nested subfolder, an empty subfolder and a vanished file.
func soundTree(t *testing.T) []SoundFolder {
	base := t.TempDir()
	a := filepath.Join(base, "a", "Sounds")
	write(t, a, "horn.mp3", "horn")
	write(t, filepath.Join(a, "Anime", "Naruto"), "jutsu.wav", strings.Repeat("wave", 1000))
	if err := os.MkdirAll(filepath.Join(a, "Empty", "Deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(base, "b", "Sounds")
	write(t, b, "bell.ogg", "bell")
	return []SoundFolder{
		{Root: a,
			Folders: []string{filepath.Join(a, "Anime"), filepath.Join(a, "Anime", "Naruto"), filepath.Join(a, "Empty"), filepath.Join(a, "Empty", "Deeper")},
			Files:   []string{filepath.Join(a, "horn.mp3"), filepath.Join(a, "Anime", "Naruto", "jutsu.wav"), filepath.Join(a, "gone.mp3")}},
		{Root: b, Files: []string{filepath.Join(b, "bell.ogg"), filepath.Join(base, "elsewhere.mp3")}},
	}
}

func TestZipSounds(t *testing.T) {
	folders := soundTree(t)
	if n, size := soundsSize(folders); n != 3 || size != 4+4000+4 {
		t.Fatalf("soundsSize = %d files, %d bytes", n, size)
	}
	dest := filepath.Join(t.TempDir(), SoundsName(t0))
	var last, total int64
	res, err := ZipSounds(context.Background(), dest, folders, func(done, all int64) { last, total = done, all })
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 3 || res.Skipped != 1 || res.Size == 0 || last != total || total != 4008 {
		t.Fatalf("result %+v, progress %d of %d", res, last, total)
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}

	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := map[string]*zip.File{}
	var names []string
	for _, f := range zr.File {
		got[f.Name] = f
		names = append(names, f.Name)
	}
	want := []string{
		"Sounds/", "Sounds/Anime/", "Sounds/Anime/Naruto/", "Sounds/Empty/", "Sounds/Empty/Deeper/",
		"Sounds/horn.mp3", "Sounds/Anime/Naruto/jutsu.wav",
		"Sounds (2)/", "Sounds (2)/bell.ogg", // same name, made unique; elsewhere.mp3 isn't below the folder
	}
	if !slices.Equal(names, want) {
		t.Fatalf("entries\n %q\nwant\n %q", names, want)
	}
	if !got["Sounds/Empty/Deeper/"].FileInfo().IsDir() {
		t.Error("empty folder isn't a folder entry")
	}
	if m := got["Sounds/horn.mp3"].Method; m != zip.Store {
		t.Errorf("mp3 method %d, want stored", m)
	}
	if m := got["Sounds/Anime/Naruto/jutsu.wav"].Method; m != zip.Deflate {
		t.Errorf("wav method %d, want deflated", m)
	}
	rc, err := got["Sounds/Anime/Naruto/jutsu.wav"].Open()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != strings.Repeat("wave", 1000) {
		t.Error("wav content differs")
	}
}

func TestZipSoundsCancelled(t *testing.T) {
	folders := soundTree(t)
	dest := filepath.Join(t.TempDir(), SoundsName(t0))
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ZipSounds(ctx, dest, folders, func(int64, int64) { cancel() })
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	for _, p := range []string{dest, dest + ".tmp"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s exists after cancelling", filepath.Base(p))
		}
	}
}

func TestTopName(t *testing.T) {
	used := map[string]bool{}
	for _, tc := range []struct{ root, want string }{
		{`C:\Users\me\Sounds`, "Sounds"},
		{`D:\sounds`, "sounds (2)"}, // Windows names are case-insensitive
		{`E:\Sounds\`, "Sounds (3)"},
		{`F:\Memes`, "Memes"},
	} {
		if got := topName(filepath.FromSlash(tc.root), used); filepath.Separator == '\\' && got != tc.want {
			t.Errorf("topName(%s) = %q, want %q", tc.root, got, tc.want)
		}
	}
	if filepath.Separator == '\\' {
		if got := topName(`G:\`, used); got != "G" {
			t.Errorf("drive root: %q, want G", got)
		}
	}
}
