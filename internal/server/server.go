// Package server hosts the phone remote: the embedded web app, the REST API
// (compatible with the Soundux Remote front end), PIN login and QR pairing.
package server

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/motivated2die/resound/internal/library"
)

// The remote's web files. Copy the Soundux Remote front end
// (src/helper/webserver/html/*) into internal/server/web/.
//
//go:embed all:web
var webFiles embed.FS

// WebFS returns the embedded web app.
func WebFS() fs.FS {
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	return sub
}

const cookieName = "resound_session"

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
	// DefaultVolumes returns the default "you hear" / "others hear"
	// volumes in percent.
	DefaultVolumes() (local, remote int)
	// SetSoundVolume applies a sound's new custom volumes (percent, nil =
	// the default) to its playing voices, previews included, so changes are
	// heard at once.
	SetSoundVolume(soundID uint64, local, remote *int)
}

// Server is the remote's HTTP server.
type Server struct {
	lib    *library.Library
	player Player
	auth   *Auth
	web    fs.FS
	logf   func(format string, args ...any)
	epoch  string // distinguishes library revisions across restarts

	mu       sync.Mutex
	httpSrv  *http.Server
	port     int
	startErr error
}

// New creates a server. logf receives diagnostic lines.
func New(lib *library.Library, player Player, auth *Auth, web fs.FS, logf func(string, ...any)) *Server {
	return &Server{lib: lib, player: player, auth: auth, web: web, logf: logf, epoch: newEpoch()}
}

// Auth returns the server's authentication state.
func (s *Server) Auth() *Auth { return s.auth }

// Start listens on all interfaces at port. A port conflict is reported
// immediately; serving continues in the background.
func (s *Server) Start(port int) error {
	s.Close()
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.port = port
	if err != nil {
		s.startErr = err
		return err
	}
	s.startErr = nil
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	s.httpSrv = srv
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logf("server: %v", err)
			s.mu.Lock()
			s.startErr = err
			s.mu.Unlock()
		}
	}()
	s.logf("server: listening on port %d", port)
	return nil
}

// Close stops the server immediately.
func (s *Server) Close() {
	s.mu.Lock()
	srv := s.httpSrv
	s.httpSrv = nil
	s.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// Status reports the port and whether the server is running.
func (s *Server) Status() (port int, running bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port, s.httpSrv != nil && s.startErr == nil, s.startErr
}

// Handler returns the full HTTP handler (exposed for tests).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	return s.middleware(mux)
}

// publicPath reports paths reachable without a session: the login page and
// what it loads, plus the auth endpoints.
func publicPath(p string) bool {
	switch {
	case p == "/login.html", p == "/manifest.json", p == "/favicon.ico", p == "/pair":
		return true
	case strings.HasPrefix(p, "/api/auth/"), strings.HasPrefix(p, "/assets/"):
		return true
	}
	return false
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		if !publicPath(r.URL.Path) && !s.authenticated(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
				return
			}
			http.Redirect(w, r, "/login.html", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && s.auth.Valid(c.Value)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode, // Lax so the QR link (a top-level navigation) keeps it
	})
}

func (s *Server) routes(mux *http.ServeMux) {
	// Auth.
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("GET /api/auth/check", s.handleCheck)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /pair", s.handlePair)

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

	// Resound additions: the whole library in one call, and the phone's
	// card colors, emoji and layouts stored on the PC (shared by all phones).
	mux.HandleFunc("GET /api/library", s.handleLibrary)
	mux.HandleFunc("POST /api/library/import", s.handleImport)
	mux.HandleFunc("POST /api/library/reset-appearance", s.handleResetAppearance)
	mux.HandleFunc("POST /api/sounds/{id}/appearance", s.handleAppearance)
	mux.HandleFunc("POST /api/layouts/{key}", s.handleSetLayout)

	// The web app.
	if _, err := fs.Stat(s.web, "index.html"); err != nil {
		mux.HandleFunc("GET /", missingWebApp)
		return
	}
	files := http.FileServerFS(s.web)
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	}))
}

func missingWebApp(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><meta name="viewport" content="width=device-width">
<title>Resound</title><body style="font-family:sans-serif;padding:24px">
<h2>Resound is running</h2><p>The remote web app was not embedded in this build.
Copy the remote front end into <code>internal/server/web/</code> and rebuild.</p>
<p>The API is available under <code>/api/</code>.</p></body>`))
}
