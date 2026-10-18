package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/motivated2die/resound/internal/importer"
	"github.com/motivated2die/resound/internal/ui"
)

// Files Resound plays are copied; files only the editor takes are cut there.
func TestEditable(t *testing.T) {
	for name, want := range map[string]bool{
		`D:\Clips\Funny.mp4`: true,
		`D:\Clips\Funny.MKV`: true,
		`D:\Clips\Note.m4a`:  true,
		`D:\Clips\Note.opus`: true,
		`D:\Sounds\Horn.mp3`: false, // plays as it is
		`D:\Sounds\Horn.ogg`: false,
		`D:\Docs\Notes.txt`:  false,
		`D:\Docs\NoExt`:      false,
	} {
		if got := editable(name); got != want {
			t.Errorf("editable(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDropHint(t *testing.T) {
	for _, tc := range []struct {
		paths []string
		want  string
	}{
		{[]string{`D:\a.mp4`}, "Drop to cut in the editor"},
		{[]string{`D:\a.mp4`, `D:\b.mov`}, "Drop to cut in the editor"},
		{[]string{`D:\a.mp4`, `D:\b.mp3`}, ""}, // sounds are copied: the usual hint
		{[]string{`D:\b.mp3`}, ""},
		{nil, ""}, // a link
	} {
		if got := dropHint(ui.DropData{Paths: tc.paths}); got != tc.want {
			t.Errorf("dropHint(%v) = %q, want %q", tc.paths, got, tc.want)
		}
	}
}

func TestAppOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://getresound.app":     "https://getresound.app",
		"https://getresound.app/":    "https://getresound.app",
		"http://localhost:8095/sub/": "http://localhost:8095",
		"not a url":                  "",
	} {
		if got := appOrigin(in); got != want {
			t.Errorf("appOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}

// Old imports make room for new ones, but a file open in the editor stays
// listed: its page still sends clips.
func TestImportsKeepEditing(t *testing.T) {
	var s importsState
	editing := s.add(&soundImport{state: importEditing})
	for range maxImportsShown + 3 {
		s.add(&soundImport{state: importDone})
	}
	if len(s.jobs) != maxImportsShown {
		t.Fatalf("%d jobs listed, want %d", len(s.jobs), maxImportsShown)
	}
	if s.jobs[0] != editing {
		t.Error("the file open in the editor was forgotten")
	}
	s.jobs = append(s.jobs, &soundImport{state: importRunning})
	s.clearFinished()
	if len(s.jobs) != 2 || s.jobs[0] != editing {
		t.Errorf("clearFinished kept %d jobs", len(s.jobs))
	}
}

// Esc pressed while cutting in the browser reaches the panel too: with a
// file open in the editor, the library page neither goes back nor closes.
func TestEscapeKeepsLibraryWhileEditing(t *testing.T) {
	a := &App{page: pageLibrary}
	a.imports.add(&soundImport{state: importEditing})
	if !a.onEscape() {
		t.Error("Esc would close the panel while a file is open in the editor")
	}
	if a.page != pageLibrary {
		t.Errorf("Esc left the library page for page %d", a.page)
	}
	if (&App{page: pageHome}).onEscape() {
		t.Error("Esc on the home page should still close the panel")
	}
}

// A save from the editor replaces a sound only once it plays: junk and
// empty saves are refused and leave nothing behind in the folder.
func TestWriteChecked(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		body string
		want error
	}{
		{"not an mp3 at all", importer.ErrUnsupported},
		{"", importer.ErrNoSound},
	} {
		if _, err := writeChecked(strings.NewReader(tc.body), dir, ".mp3"); !errors.Is(err, tc.want) {
			t.Errorf("writeChecked(%q) = %v, want %v", tc.body, err, tc.want)
		}
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}
