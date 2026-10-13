// Command devremote serves the phone remote (web app, API, library and
// indexer) without the Windows tray, outputs or routing, so
// the front end and the library can be worked on and tested on any system.
// Playing a sound only logs it.
//
//	go run ./cmd/devremote -folder D:\Sounds -port 8090
//
// It also accepts the installable app over WebRTC and prints its pairing
// link (go run ./cmd/webapp -serve :8095 serves the app on localhost).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/server"
)

type folders []string

func (f *folders) String() string     { return strings.Join(*f, ",") }
func (f *folders) Set(v string) error { *f = append(*f, v); return nil }

// logPlayer pretends to play.
type logPlayer struct{ lib *library.Library }

func (p logPlayer) Play(id uint64, preview bool) (server.Started, error) {
	s, _, ok := p.lib.Sound(id)
	if !ok {
		return server.Started{}, server.ErrNotFound
	}
	log.Printf("play %q (preview=%v, %v)", s.Name, preview, s.Duration)
	return server.Started{PlayingID: uint64(time.Now().UnixNano()), Length: s.Duration}, nil
}
func (logPlayer) StopAll()                            {}
func (logPlayer) Progress() []server.Progress         { return nil }
func (logPlayer) TogglePause() bool                   { return false }
func (logPlayer) SetTalkThrough(bool)                 {}
func (logPlayer) DefaultVolumes() (local, remote int) { return 50, 100 }
func (logPlayer) SetSoundVolume(id uint64, local, remote *int) {
	log.Printf("volume of %d now %v / %v", id, deref(local), deref(remote))
}

func deref(v *int) any {
	if v == nil {
		return "default"
	}
	return *v
}

func main() {
	var dirs folders
	data := flag.String("data", filepath.Join(os.TempDir(), "resound-dev"), "data folder (library.json, remote.json)")
	port := flag.Int("port", 8090, "HTTP port (and UDP port for the app)")
	appURL := flag.String("app", "http://localhost:8095", "address of the installable app (go run ./cmd/webapp -serve :8095)")
	flag.Var(&dirs, "folder", "sound folder (repeatable)")
	flag.Parse()

	lib := library.Open(*data, audio.SupportedExtensions)
	lib.Logf = log.Printf
	defer lib.Close()
	ix := library.NewIndexer(lib, audio.ProbeDuration)
	ix.Logf = log.Printf
	lib.OnChange = ix.Kick
	lib.Scan(dirs)
	ix.Start()

	auth, err := server.NewAuth(*data)
	if err != nil {
		log.Fatal(err)
	}
	srv := server.New(lib, logPlayer{lib}, auth, server.WebFS(), log.Printf)
	srv.Name, _ = os.Hostname()
	if err := srv.Start(*port); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("PIN %s\nhttp://localhost:%d/pair?code=%s\n", auth.PIN(), *port, auth.PairCode())
	var addrs []string
	for _, a := range server.LocalAddresses() {
		if !a.Other {
			addrs = append(addrs, a.IP)
		}
	}
	if link, ok := srv.AppPairURL(*appURL, addrs); ok {
		fmt.Printf("app (same pairing code, works once): %s\n", link)
	} else {
		fmt.Println("app: not available (see the log)")
	}
	select {}
}
