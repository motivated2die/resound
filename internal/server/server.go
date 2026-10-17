// Package server is the PC's side of the Resound app: the remote API
// (compatible with the Soundux Remote front end), served over WebRTC data
// channels (rtc.go), and app pairing.
package server

import (
	"errors"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/motivated2die/resound/internal/library"
)

// ErrNotFound is returned by a Player for unknown sound IDs.
var ErrNotFound = errors.New("sound not found")

// ErrUnplayable is returned (wrapped) by a Player when a sound's file is
// missing or damaged. The phone shows it for that sound; the server is fine.
var ErrUnplayable = errors.New("sound file unplayable")

// Started describes a sound that began playing.
type Started struct {
	PlayingID uint64
	Length    time.Duration
}

// Progress is one playing sound, as the phone's progress bars need it.
type Progress struct {
	PlayingID uint64
	SoundID   uint64
	Name      string
	Position  time.Duration
	Length    time.Duration
	Paused    bool
}

// Player is implemented by the app; it owns playback, the mic and volumes.
// Every method may be called from any goroutine.
type Player interface {
	Play(soundID uint64, preview bool) (Started, error)
	StopAll()
	Progress() []Progress
	// TogglePause pauses everything playing, or resumes it. It returns the
	// new state: true = paused.
	TogglePause() bool
	SetTalkThrough(on bool)
	// TogglePanel opens the PC's panel, or closes it if it is open, and
	// returns whether it is open now.
	TogglePanel() bool
	// DefaultVolumes returns the default "you hear" / "others hear"
	// volumes in percent.
	DefaultVolumes() (local, remote int)
	// SetSoundVolume applies a sound's new custom volumes (percent, nil =
	// the default) to its playing voices, previews included, so changes are
	// heard at once.
	SetSoundVolume(soundID uint64, local, remote *int)
}

// Server accepts the app's connections and answers its API requests.
type Server struct {
	lib    *library.Library
	player Player
	auth   *Auth
	logf   func(format string, args ...any)
	epoch  string // distinguishes library revisions across restarts

	// Name is this PC's name, shown in the app.
	Name string

	handlerOnce sync.Once
	handlerVal  http.Handler

	mu     sync.Mutex
	port   int
	rtc    *RTC
	rtcErr error
}

// New creates a server. logf receives diagnostic lines.
func New(lib *library.Library, player Player, auth *Auth, logf func(string, ...any)) *Server {
	return &Server{lib: lib, player: player, auth: auth, logf: logf, epoch: newEpoch()}
}

// Auth returns the server's pairing state.
func (s *Server) Auth() *Auth { return s.auth }

// Start listens for the app on UDP port, on all interfaces.
func (s *Server) Start(port int) error {
	s.Close()
	s.mu.Lock()
	rtc := s.rtc
	s.mu.Unlock()
	var err error
	if rtc == nil {
		if rtc, err = newRTC(s); err != nil {
			s.logf("app: %v", err)
		}
	}
	if err == nil {
		if err = rtc.listen(port); err != nil {
			s.logf("app: cannot listen on UDP port %d: %v", port, err)
		} else {
			s.logf("app: listening on UDP port %d", port)
		}
	}
	s.mu.Lock()
	s.port, s.rtc, s.rtcErr = port, rtc, err
	s.mu.Unlock()
	return err
}

// SignOut forgets one paired device and ends its open connections, so it
// has to pair again.
func (s *Server) SignOut(id string) {
	s.auth.ForgetDevice(id)
	if rtc := s.currentRTC(); rtc != nil {
		rtc.closeDevice(id)
	}
}

// SignOutAll signs out every device (Auth.ResetAll) and ends their open
// connections.
func (s *Server) SignOutAll() error {
	err := s.auth.ResetAll()
	if rtc := s.currentRTC(); rtc != nil {
		rtc.closeDevice("")
	}
	return err
}

// Connected returns the IDs of the devices connected right now.
func (s *Server) Connected() map[string]bool {
	out := map[string]bool{}
	if rtc := s.currentRTC(); rtc != nil {
		for _, sess := range rtc.signedIn("") {
			sess.mu.Lock()
			out[sess.device] = true
			sess.mu.Unlock()
		}
	}
	return out
}

func (s *Server) currentRTC() *RTC {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rtc
}

// Close stops listening and ends all connections.
func (s *Server) Close() {
	s.mu.Lock()
	rtc := s.rtc
	s.mu.Unlock()
	if rtc != nil {
		rtc.close()
	}
}

// LibraryChanged tells the connected apps that tabs or sounds changed (a
// refresh, added or removed folders, an imported sound), so they reload the
// library now instead of on their next tab switch or start. The event
// carries the library's ETag: an app that has that revision already skips
// the reload. Sending never waits for the apps.
func (s *Server) LibraryChanged() {
	s.mu.Lock()
	rtc := s.rtc
	s.mu.Unlock()
	if rtc == nil {
		return
	}
	ev := map[string]any{"t": "event", "e": "library", "rev": s.libraryETag()}
	go func() {
		if n := rtc.broadcast(ev); n > 0 {
			s.logf("app: library changed, told %d device(s)", n)
		}
	}()
}

// Status reports the port and whether the app can connect.
func (s *Server) Status() (port int, running bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port, s.rtc != nil && s.rtcErr == nil && s.rtc.Port() != 0, s.rtcErr
}

// PairURL is the link in the pairing QR code: the app's address, then, in
// the fragment (which browsers never send to the host), this PC's addresses,
// port, certificate fingerprint, a one-time pairing code and name. ok is
// false while the app can't connect.
func (s *Server) PairURL(appURL string, addrs []string) (url string, ok bool) {
	s.mu.Lock()
	rtc, rtcErr := s.rtc, s.rtcErr
	s.mu.Unlock()
	if rtc == nil || rtcErr != nil || rtc.Port() == 0 || len(addrs) == 0 {
		return "", false
	}
	q := neturl.Values{}
	q.Set("v", strconv.Itoa(rtcProtocol))
	q.Set("a", strings.Join(addrs, ","))
	q.Set("p", strconv.Itoa(rtc.Port()))
	q.Set("f", rtc.Fingerprint())
	q.Set("c", s.auth.PairCode())
	if s.Name != "" {
		q.Set("n", s.Name)
	}
	return strings.TrimRight(appURL, "/") + "/#pair&" + q.Encode(), true
}

// Handler returns the API handler. Requests reach it from the app's
// connections, marked with the device (rtc.go); others are refused.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rtcDevice(r) == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// handler is Handler, built once.
func (s *Server) handler() http.Handler {
	s.handlerOnce.Do(func() { s.handlerVal = s.Handler() })
	return s.handlerVal
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

	// Library and playback (the Soundux Remote API).
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /api/tabs", s.handleTabs)
	mux.HandleFunc("GET /api/tabs/{id}/sounds", s.handleTabSounds)
	mux.HandleFunc("GET /api/favorites", s.handleFavorites)
	mux.HandleFunc("GET /api/sounds/progress", s.handleProgress)
	mux.HandleFunc("GET /api/sounds/settings", s.handleSoundSettings)
	mux.HandleFunc("GET /api/sounds/{id}", s.handleSound)
	mux.HandleFunc("POST /api/sounds/{id}/play", func(w http.ResponseWriter, r *http.Request) { s.handlePlay(w, r, false) })
	mux.HandleFunc("POST /api/sounds/{id}/preview", func(w http.ResponseWriter, r *http.Request) { s.handlePlay(w, r, true) })
	mux.HandleFunc("POST /api/sounds/stop", s.handleStop)
	mux.HandleFunc("POST /api/sounds/{id}/favorite", s.handleFavorite)
	mux.HandleFunc("POST /api/sounds/{id}/volume", s.handleVolume)
	mux.HandleFunc("POST /api/sounds/{id}/volume/reset", s.handleVolumeReset)
	mux.HandleFunc("POST /api/playback/toggle", s.handleToggle)
	mux.HandleFunc("POST /api/talkthrough/start", func(w http.ResponseWriter, r *http.Request) { s.handleTalk(w, true) })
	mux.HandleFunc("POST /api/talkthrough/stop", func(w http.ResponseWriter, r *http.Request) { s.handleTalk(w, false) })
	mux.HandleFunc("POST /api/panel/toggle", s.handlePanelToggle)

	// Resound additions: the whole library in one call, and the phone's
	// card colors, emoji and layouts stored on the PC (shared by all phones).
	mux.HandleFunc("GET /api/library", s.handleLibrary)
	mux.HandleFunc("POST /api/library/import", s.handleImport)
	mux.HandleFunc("POST /api/library/reset-appearance", s.handleResetAppearance)
	mux.HandleFunc("POST /api/library/tab-order", s.handleTabOrder)
	mux.HandleFunc("POST /api/sounds/{id}/appearance", s.handleAppearance)
	mux.HandleFunc("POST /api/layouts/{key}", s.handleSetLayout)
}
