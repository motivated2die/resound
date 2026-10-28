// Command devremote is the PC side of the app (API, pairing, library and
// indexer) without the Windows tray, outputs or routing, so the app and the
// library can be worked on and tested on any system. Playing a sound is
// logged and timed, nothing is heard. It prints a pairing link for the app,
// by default the local one:
//
//	go run ./cmd/webapp -serve :8095
//	go run ./cmd/devremote -folder D:\Sounds -port 8090
//
// Pressing Enter refreshes the library like the panel's Refresh button,
// including the event that makes connected apps reload it. "l" and Enter
// holds or lets go of the launchpad on the app in use, as its key on the PC
// would; "<" and ">" switch its tab meanwhile.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	lp := &devLaunchpad{lib: lib, srv: srv}
	srv.OnView = lp.follow
	// Enter does what the panel's Refresh does: rescan, and tell the
	// connected apps to reload the library.
	fmt.Println("press Enter to refresh the library (as the panel's Refresh button); l, < and > Enter: the launchpad")
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		switch strings.TrimSpace(in.Text()) {
		case "l":
			lp.toggle()
		case "<":
			lp.step(-1)
		case ">":
			lp.step(1)
		default:
			n := lib.Scan(dirs)
			log.Printf("refresh: %d sound(s)", n)
			srv.LibraryChanged()
		}
	}
	select {} // stdin closed (e.g. started without a console): keep serving
}

// devLaunchpad holds the launchpad as the PC's app does (app/launchpad.go):
// the app in use labels its tab's buttons and follows tab switches.
type devLaunchpad struct {
	lib  *library.Library
	srv  *server.Server
	mu   sync.Mutex
	on   bool
	conn uint64
	tab  string
}

const devLaunchpadKeys = "1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ"

func (d *devLaunchpad) toggle() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.on = !d.on
	if !d.on {
		d.srv.SendLaunchpad(d.conn, server.Launchpad{Tab: d.tab})
		log.Printf("launchpad: off")
		return
	}
	v, ok := d.srv.ActiveView()
	if !ok {
		log.Printf("launchpad: no app connected")
		d.on = false
		return
	}
	d.conn, d.tab = v.Conn, v.Tab
	d.send()
}

func (d *devLaunchpad) step(n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.on {
		return
	}
	tabs := []string{library.FavoritesKey}
	for _, t := range d.lib.PhoneTabs() {
		tabs = append(tabs, strconv.FormatUint(t.ID, 10))
	}
	i := max(0, slices.Index(tabs, d.tab))
	d.tab = tabs[max(0, min(len(tabs)-1, i+n))]
	d.send()
}

// follow takes up the app in use and its tab while the launchpad is held.
func (d *devLaunchpad) follow() {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.srv.ActiveView()
	if !d.on || !ok || v.Conn == d.conn && v.Tab == d.tab {
		return
	}
	if v.Conn != d.conn {
		d.srv.SendLaunchpad(d.conn, server.Launchpad{Tab: d.tab})
	}
	d.conn, d.tab = v.Conn, v.Tab
	d.send()
}

func (d *devLaunchpad) send() {
	sounds, ok := d.lib.ShownSounds(d.tab)
	if !ok {
		d.tab = library.FavoritesKey
		sounds, _ = d.lib.ShownSounds(d.tab)
	}
	keys := map[uint64]string{}
	for i, s := range sounds[:min(len(sounds), len(devLaunchpadKeys))] {
		keys[s.ID] = devLaunchpadKeys[i : i+1]
	}
	d.srv.SendLaunchpad(d.conn, server.Launchpad{On: true, Tab: d.tab, Keys: keys})
	log.Printf("launchpad: on app %d, tab %s, %d key(s)", d.conn, d.tab, len(keys))
}
