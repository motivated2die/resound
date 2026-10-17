package app

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/backup"
	"github.com/motivated2die/resound/internal/config"
	"github.com/motivated2die/resound/internal/library"
)

// TestSoundsZipMirrorsLibrary zips a scanned library folder and checks the
// zip holds what the library imports: sounds at any depth and every folder,
// empty ones too, but not hidden folders or files that aren't sounds.
func TestSoundsZipMirrorsLibrary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Sounds")
	for name, data := range map[string]string{
		"horn.mp3":             "horn",
		"notes.txt":            "not a sound",
		"Anime/Naruto/a.wav":   "jutsu",
		".git/hidden.mp3":      "skipped like in the library",
		"~resound-partial.mp3": "a download in progress",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	folder := root + string(filepath.Separator) // as a config may hold it
	lib := library.Open(t.TempDir(), audio.SupportedExtensions)
	defer lib.Close()
	lib.Scan([]string{folder})
	a := &App{cfg: config.Config{Folders: []string{folder}}, lib: lib}

	folders, files, size := a.soundFolders()
	if files != 2 || size != int64(len("horn")+len("jutsu")) {
		t.Fatalf("%d files, %d bytes", files, size)
	}
	dest := filepath.Join(t.TempDir(), "sounds.zip")
	if _, err := backup.ZipSounds(context.Background(), dest, folders, nil); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	want := []string{"Sounds/", "Sounds/Anime/", "Sounds/Anime/Naruto/", "Sounds/Anime/Naruto/a.wav", "Sounds/Empty/", "Sounds/horn.mp3"}
	if !slices.Equal(names, want) {
		t.Fatalf("entries %q, want %q", names, want)
	}
}
