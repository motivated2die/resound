// Command devedit is the PC side of cutting a sound in the editor, without
// the panel: it offers one file to the app's edit page as Resound does when
// a video is dropped onto a folder (package editbridge), saves every clip
// into a folder and prints the page's address, by default the local app's:
//
//	go run ./cmd/webapp -serve :8095
//	go run ./cmd/devedit -file D:\clip.mp4 -out D:\Sounds
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

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/editbridge"
	"github.com/motivated2die/resound/internal/importer"
)

func main() {
	file := flag.String("file", "", "the file to cut sounds from")
	out := flag.String("out", ".", "the folder clips are saved into")
	app := flag.String("app", "http://localhost:8095", "the app's address")
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
	token, port, err := b.Add(*file, *out)
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
