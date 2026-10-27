// Package editbridge hands a file Resound can't play (a video, or audio in
// another format) to the browser editor and takes the clips cut from it
// back. The editor is audiocutter.online, embedded in the app's /edit page
// (webapp/src/edit.html), which the panel opens in a browser window.
//
// The page and Resound talk over a small HTTP server on 127.0.0.1 that
// runs only while a file is being edited. The page reads the file from it
// and posts each clip back to it; the file goes from this PC into the
// browser and back, never through the app's host. Each file gets a random
// token in its URL, which the page carries in its fragment (never sent to
// the host). Requests must come from the app's own origin, name this
// server's address in their Host header (no DNS rebinding) and carry a
// known token.
package editbridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MaxSourceBytes limits the file handed to the editor, which reads it
	// into the browser's memory whole.
	MaxSourceBytes = 1 << 30
	// Format is the format new clips come back in: lossless, so a clip isn't
	// encoded lossily a second time, and readable at every sample rate.
	Format = "flac"
	// SampleRate is the rate new clips are rendered at: Resound's mixer
	// runs at it, so they play without resampling, whatever rate the editor
	// would read from the source. A sound of the library keeps its own.
	SampleRate = 48000
	// closeGrace is how long a page that went away (closed, or reloading)
	// has to come back before its file counts as closed.
	closeGrace = 10 * time.Second
	// stopGrace is how long a file stopped in the panel waits for its page
	// to finish (and have its window closed) before it is removed anyway.
	stopGrace = 5 * time.Second
)

// windowTitle is the title a finishing page that couldn't close its own
// window gives it, so Resound can close exactly that window
// (Options.CloseWindow): a fixed text and 16 random hex digits.
var windowTitle = regexp.MustCompile(`^Resound · Closing [0-9a-f]{16}$`)

// ErrTooLarge means a file is larger than MaxSourceBytes.
var ErrTooLarge = errors.New("the file is too large for the editor")

// Options are the Bridge's callbacks. They run on the server's goroutines.
type Options struct {
	// Origins are the pages that may use the bridge, e.g.
	// "https://getresound.app".
	Origins []string
	// OnOpen says that the page has loaded the file with this token (again,
	// after a reload).
	OnOpen func(token string)
	// OnResult saves a clip cut from the job's file and returns the name it
	// was saved under. Its error's text is shown to the user in the page.
	OnResult func(job Job, body io.Reader) (name string, err error)
	// OnClose says that the page of this file was closed, or finished. The
	// file is no longer served.
	OnClose func(token string)
	// CloseWindow closes the browser window titled title, if Resound can:
	// a page that finished asks for it when the browser refused to let it
	// close its own window. title matches windowTitle. It runs before
	// OnClose.
	CloseWindow func(title string)
}

// Job is a file being edited.
type Job struct {
	Token string
	Path  string // the file
	Dir   string // the folder clips go into
	// Replace: the file is a sound of the library, and what the editor
	// saves replaces it, in its own format (Format otherwise). The editor
	// reads the right sample rate from the headers of the formats Resound
	// plays.
	Replace bool
	Format  string
	size    int64
	closer  *time.Timer // set while the page is away
	// stopped: editing was stopped in the panel. The page is told (state)
	// and may still finish; nothing else is served.
	stopped bool
}

// Bridge serves the files being edited. Its zero value is not usable; use New.
type Bridge struct {
	opts Options

	mu   sync.Mutex
	jobs map[string]*Job
	srv  *http.Server
	port int
}

// New returns a Bridge. Its server starts with the first file.
func New(opts Options) *Bridge {
	return &Bridge{opts: opts, jobs: map[string]*Job{}}
}

// Add offers a file to the editor and returns the token and port the page
// needs (see PageURL). Clips cut from it go into dir; the page shows its
// name. With replace, what the editor saves replaces the file (Job).
func (b *Bridge) Add(path, dir string, replace bool) (token string, port int, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	if info.Size() > MaxSourceBytes {
		return "", 0, ErrTooLarge
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", 0, err
	}
	token = hex.EncodeToString(raw[:])

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.srv == nil {
		if err := b.start(); err != nil {
			return "", 0, err
		}
	}
	format := Format
	if replace {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	b.jobs[token] = &Job{Token: token, Path: path, Dir: dir, Replace: replace, Format: format, size: info.Size()}
	return token, b.port, nil
}

// PageURL is the address of the app's edit page for a file: app is the
// app's address, e.g. "https://getresound.app". Port and token are in the
// fragment, which the browser keeps to itself.
func PageURL(app string, port int, token string) string {
	return strings.TrimSuffix(app, "/") + "/edit#v=1&p=" + strconv.Itoa(port) + "&t=" + token
}

// Remove stops serving a file. The server stops with the last one.
func (b *Bridge) Remove(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remove(token)
}

func (b *Bridge) remove(token string) {
	if j := b.jobs[token]; j != nil && j.closer != nil {
		j.closer.Stop()
	}
	delete(b.jobs, token)
	if len(b.jobs) == 0 && b.srv != nil {
		b.srv.Close()
		b.srv, b.port = nil, 0
	}
}

// Stop ends editing a file from Resound's side: the page learns it on its
// next state check and finishes, which closes its window. A page that
// doesn't (closed already, or gone) is dropped after stopGrace. OnClose
// doesn't run for it.
func (b *Bridge) Stop(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	j := b.jobs[token]
	if j == nil {
		return
	}
	j.stopped = true
	if j.closer != nil {
		j.closer.Stop()
	}
	j.closer = time.AfterFunc(stopGrace, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.jobs[token] == j {
			b.remove(token)
		}
	})
}

// Close stops serving every file.
func (b *Bridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for token := range b.jobs {
		b.remove(token)
	}
}

// start listens on a free port of the loopback address. b.mu is held.
func (b *Bridge) start() error {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	b.port = ln.Addr().(*net.TCPAddr).Port
	b.srv = &http.Server{
		Handler:           b.handler(b.port),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go b.srv.Serve(ln)
	return nil
}

// handler serves /v1/<token> (the file's details), /v1/<token>/source (the
// file), /v1/<token>/result (POST: a clip), /v1/<token>/state (whether
// editing was stopped in the panel), /v1/<token>/done (POST: the user
// finished; with a window title, the window should close) and /v1/<token>/close (POST: the page
// is going away) for a server on port.
func (b *Bridge) handler(port int) http.Handler {
	host := "127.0.0.1:" + strconv.Itoa(port)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if r.Host != host || !slices.Contains(b.opts.Origins, origin) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Vary", "Origin")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, POST")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				h.Set("Access-Control-Allow-Private-Network", "true")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		token, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v1/"), "/")
		b.mu.Lock()
		job := b.jobs[token]
		var j Job
		if job != nil {
			j = *job
		}
		b.mu.Unlock()
		if job == nil || !strings.HasPrefix(r.URL.Path, "/v1/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Resound is no longer waiting for this file. Drop it onto a folder again."})
			return
		}

		if j.stopped && action != "state" && action != "done" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Editing was stopped in the Resound panel."})
			return
		}
		switch {
		case action == "state" && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]bool{"stopped": j.stopped})
		case action == "done" && r.Method == http.MethodPost:
			// Without a body: the page closed its own window.
			var req struct {
				Window string `json:"window"`
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<10))
			if err == nil && len(body) > 0 {
				if err = json.Unmarshal(body, &req); err == nil && !windowTitle.MatchString(req.Window) {
					err = errors.New("bad window title")
				}
			}
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			b.finish(token, req.Window)
		case action == "" && r.Method == http.MethodGet:
			b.opened(token)
			writeJSON(w, http.StatusOK, map[string]any{
				"filename": filepath.Base(j.Path),
				"name":     strings.TrimSuffix(filepath.Base(j.Path), filepath.Ext(j.Path)),
				"folder":   filepath.Base(j.Dir),
				"size":     j.size,
				"type":     contentType(j.Path),
				"format":   j.Format,
				"rate":     j.rate(),
				"replace":  j.Replace,
			})
		case action == "source" && r.Method == http.MethodGet:
			f, err := os.Open(j.Path)
			if err != nil {
				writeJSON(w, http.StatusGone, map[string]string{"error": "The file can't be read any more: it was moved or deleted."})
				return
			}
			defer f.Close()
			h.Set("Content-Type", contentType(j.Path))
			http.ServeContent(w, r, "", time.Time{}, f)
		case action == "result" && r.Method == http.MethodPost:
			name, err := b.opts.OnResult(j, r.Body)
			if err != nil {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"name": name})
		case action == "close" && r.Method == http.MethodPost:
			b.closing(token)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})
}

// rate is the sample rate the editor renders the job's saves at; 0 leaves
// it to the editor, which keeps the source's (a sound of the library).
func (j Job) rate() int {
	if j.Replace {
		return 0
	}
	return SampleRate
}

// finish ends a file whose page is done: it is removed at once (no grace,
// as for a page that went away), and its window is closed if the page
// names it (window; "" when the page closed it itself).
func (b *Bridge) finish(token, window string) {
	b.mu.Lock()
	j := b.jobs[token]
	stopped := j != nil && j.stopped
	if j != nil {
		b.remove(token)
	}
	b.mu.Unlock()
	if j == nil {
		return
	}
	if window != "" && b.opts.CloseWindow != nil {
		b.opts.CloseWindow(window)
	}
	if !stopped && b.opts.OnClose != nil { // a stop came from the panel, which knows
		b.opts.OnClose(token)
	}
}

// opened notes that the page has the file (again): a pending close is off.
func (b *Bridge) opened(token string) {
	b.mu.Lock()
	if j := b.jobs[token]; j != nil && j.closer != nil {
		j.closer.Stop()
		j.closer = nil
	}
	b.mu.Unlock()
	if b.opts.OnOpen != nil {
		b.opts.OnOpen(token)
	}
}

// closing notes that the page is going away. Unless it comes back within
// closeGrace (a reload), the file is removed and OnClose runs.
func (b *Bridge) closing(token string) { b.closingAfter(token, closeGrace) }

func (b *Bridge) closingAfter(token string, grace time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	j := b.jobs[token]
	if j == nil {
		return
	}
	if j.closer != nil {
		j.closer.Stop()
	}
	j.closer = time.AfterFunc(grace, func() {
		b.mu.Lock()
		current := b.jobs[token] == j && j.closer != nil
		if current {
			b.remove(token)
		}
		b.mu.Unlock()
		if current && b.opts.OnClose != nil {
			b.opts.OnClose(token)
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// mediaTypes are the types of files the editor takes, by extension.
// Windows' own table (mime.TypeByExtension) depends on installed apps.
var mediaTypes = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".mkv": "video/x-matroska",
	".webm": "video/webm", ".avi": "video/x-msvideo", ".wmv": "video/x-ms-wmv", ".flv": "video/x-flv",
	".3gp": "video/3gpp", ".mpg": "video/mpeg", ".mpeg": "video/mpeg", ".ts": "video/mp2t", ".mts": "video/mp2t",
	".m4a": "audio/mp4", ".aac": "audio/aac", ".opus": "audio/opus", ".weba": "audio/webm", ".oga": "audio/ogg",
	".wma": "audio/x-ms-wma", ".aiff": "audio/aiff", ".aif": "audio/aiff", ".amr": "audio/amr",
	".mka": "audio/x-matroska", ".ac3": "audio/ac3", ".caf": "audio/x-caf",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".flac": "audio/flac", ".ogg": "audio/ogg",
}

// Editable reports whether the editor takes a file of this name.
func Editable(path string) bool {
	_, ok := mediaTypes[strings.ToLower(filepath.Ext(path))]
	return ok
}

func contentType(path string) string {
	if t, ok := mediaTypes[strings.ToLower(filepath.Ext(path))]; ok {
		return t
	}
	return "application/octet-stream"
}
