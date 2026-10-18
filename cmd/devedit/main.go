// Command devedit is the PC side of cutting a sound in the editor, without
// the panel: it offers one file to the app's edit page as Resound does when
// a video is dropped onto a folder (package editbridge), saves every clip
// into a folder and prints the page's address, by default the local app's:
//
//	go run ./cmd/webapp -serve :8095
//	go run ./cmd/devedit -file D:\clip.mp4 -out D:\Sounds
//
// With -replace, the file is a sound of the library being edited, as from
// its scissors on the library page: each save replaces it.
//
// It runs until Ctrl+C, or until the page is closed.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/editbridge"
	"github.com/motivated2die/resound/internal/importer"
)

func main() {
	file := flag.String("file", "", "the file to cut sounds from")
	out := flag.String("out", ".", "the folder clips are saved into")
	app := flag.String("app", "http://localhost:8095", "the app's address")
	replace := flag.Bool("replace", false, "edit the file itself: each save replaces it")
	flag.Parse()
	if *file == "" {
		flag.Usage()
		os.Exit(2)
	}
	u, err := url.Parse(*app)
	if err != nil || u.Host == "" {
		log.Fatalf("-app %q is not an address", *app)
	}

	closed := make(chan struct{})
	b := editbridge.New(editbridge.Options{
		Origins: []string{u.Scheme + "://" + u.Host},
		OnOpen:  func(string) { log.Print("the page has the file") },
		OnResult: func(j editbridge.Job, body io.Reader) (string, error) {
			data, err := io.ReadAll(io.LimitReader(body, 100<<20))
			if err != nil {
				return "", err
			}
			if j.Replace {
				return replaceFile(j.Path, data)
			}
			saved, err := importer.Save(bytes.NewReader(data), "."+editbridge.Format, "Clip", j.Dir, 100<<20, audio.CheckFile)
			if err != nil {
				// Kept for a look with ffprobe.
				kept, _ := os.CreateTemp(j.Dir, "refused-*.bin")
				if kept != nil {
					kept.Write(data)
					kept.Close()
					log.Printf("clip refused: %v (kept as %s)", err, kept.Name())
				}
				return "", err
			}
			log.Printf("clip saved: %s", saved.Path)
			return saved.Name, nil
		},
		OnClose: func(string) { close(closed) },
	})
	dir := *out
	if *replace {
		dir = filepath.Dir(*file)
	}
	token, port, err := b.Add(*file, dir, *replace)
	if err != nil {
		log.Fatal(err)
	}
	defer b.Close()
	fmt.Println(editbridge.PageURL(*app, port, token))

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	select {
	case <-interrupt:
	case <-closed:
		log.Print("the page was closed")
	}
}

// replaceFile writes a save over the edited file once it plays, as the
// panel does.
func replaceFile(path string, data []byte) (string, error) {
	ext := filepath.Ext(path)
	tmp := filepath.Join(filepath.Dir(path), "~resound-replace"+ext)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := audio.CheckFile(tmp); err != nil {
		os.Remove(tmp)
		log.Printf("save refused: %v", err)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	log.Printf("replaced %s (%d bytes)", path, len(data))
	return strings.TrimSuffix(filepath.Base(path), ext), nil
}
