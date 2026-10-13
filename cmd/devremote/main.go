// Command devremote is the PC side of the app (API, pairing, library and
// indexer) without the Windows tray, outputs or routing, so the app and the
// library can be worked on and tested on any system. Playing a sound only
// logs it. It prints a pairing link for the app, by default the local one:
//
//	go run ./cmd/webapp -serve :8095
//	go run ./cmd/devremote -folder D:\Sounds -port 8090
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
	port := flag.Int("port", 8090, "UDP port the app connects to")
	appURL := flag.String("app", "http://localhost:8095", "the app's address (go run ./cmd/webapp -serve :8095 serves it locally)")
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
	srv := server.New(lib, logPlayer{lib}, auth, log.Printf)
	srv.Name, _ = os.Hostname()
	if err := srv.Start(*port); err != nil {
		log.Fatal(err)
	}
	var addrs []string
	for _, a := range server.LocalAddresses() {
		if !a.Other {
			addrs = append(addrs, a.IP)
		}
	}
	if link, ok := srv.PairURL(*appURL, addrs); ok {
		fmt.Printf("pairing link (works once, for 10 minutes):\n%s\n", link)
	} else {
		fmt.Println("pairing: not available (see the log)")
	}
	select {}
}
