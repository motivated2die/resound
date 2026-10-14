// Command devremote is the PC side of the app (API, pairing, library and
// indexer) without the Windows tray, outputs or routing, so the app and the
// library can be worked on and tested on any system. Playing a sound is
// logged and timed, nothing is heard. It prints a pairing link for the app,
// by default the local one:
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
	"sync"
	"time"

	"github.com/motivated2die/resound/internal/audio"
	"github.com/motivated2die/resound/internal/library"
	"github.com/motivated2die/resound/internal/server"
)

type folders []string

func (f *folders) String() string     { return strings.Join(*f, ",") }
func (f *folders) Set(v string) error { *f = append(*f, v); return nil }

// logPlayer pretends to play: nothing is heard, but a sound counts as
// playing for its length, so the app shows progress and its end as with the
// real PC (stop and pause included).
type logPlayer struct {
	lib *library.Library

	mu      sync.Mutex
	nextID  uint64
	playing []*fakeVoice
	paused  time.Time // when everything was paused; zero while playing
	panel   bool      // the pretend panel is open
}

type fakeVoice struct {
	id, soundID uint64
	name        string
	length      time.Duration
	start       time.Time // shifted forward by pauses
}

func newLogPlayer(lib *library.Library) *logPlayer { return &logPlayer{lib: lib} }

func (p *logPlayer) Play(id uint64, preview bool) (server.Started, error) {
	s, _, ok := p.lib.Sound(id)
	if !ok {
		return server.Started{}, server.ErrNotFound
	}
	log.Printf("play %q (preview=%v, %v)", s.Name, preview, s.Duration)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	length := s.Duration
	if length <= 0 {
		length = 3 * time.Second // not indexed yet
	}
	p.playing = append(p.playing, &fakeVoice{id: p.nextID, soundID: id, name: s.Name, length: length, start: time.Now()})
	return server.Started{PlayingID: p.nextID, Length: length}, nil
}

func (p *logPlayer) StopAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.playing, p.paused = nil, time.Time{}
}

func (p *logPlayer) Progress() []server.Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if !p.paused.IsZero() {
		now = p.paused
	}
	var out []server.Progress
	kept := p.playing[:0]
	for _, v := range p.playing {
		pos := now.Sub(v.start)
		if pos >= v.length {
			continue // finished
		}
		kept = append(kept, v)
		out = append(out, server.Progress{PlayingID: v.id, SoundID: v.soundID, Name: v.name,
			Position: pos, Length: v.length, Paused: !p.paused.IsZero()})
	}
	p.playing = kept
	return out
}

func (p *logPlayer) TogglePause() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.paused.IsZero() {
		p.paused = time.Now()
		return true
	}
	for _, v := range p.playing {
		v.start = v.start.Add(time.Since(p.paused))
	}
	p.paused = time.Time{}
	return false
}

func (*logPlayer) SetTalkThrough(bool) {}
func (p *logPlayer) TogglePanel() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.panel = !p.panel
	log.Printf("panel %s (pretend)", map[bool]string{true: "opened", false: "closed"}[p.panel])
	return p.panel
}
func (*logPlayer) DefaultVolumes() (local, remote int) { return 50, 100 }
func (*logPlayer) SetSoundVolume(id uint64, local, remote *int) {
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
	srv := server.New(lib, newLogPlayer(lib), auth, log.Printf)
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
