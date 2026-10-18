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
	// Format is the format new clips come back in. The editor renders at the
	// sample rate it guesses from the file's first bytes, which for videos
	// can be any MP3 rate down to 8 kHz; Resound's MP3 decoder reads only
	// MPEG-1 (32 kHz and up), FLAC every rate. FLAC is lossless too, so a
	// clip isn't encoded lossily a second time.
	Format = "flac"
	// closeGrace is how long a page that went away (closed, or reloading)
	// has to come back before its file counts as closed.
	closeGrace = 10 * time.Second
)

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
	// OnClose says that the page of this file was closed. The file is no
	// longer served.
	OnClose func(token string)
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
// file), /v1/<token>/result (POST: a clip) and /v1/<token>/close (POST: the
// page is going away) for a server on port.
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

		switch {
		case action == "" && r.Method == http.MethodGet:
			b.opened(token)
			writeJSON(w, http.StatusOK, map[string]any{
				"filename": filepath.Base(j.Path),
				"name":     strings.TrimSuffix(filepath.Base(j.Path), filepath.Ext(j.Path)),
				"folder":   filepath.Base(j.Dir),
				"size":     j.size,
				"type":     contentType(j.Path),
				"format":   j.Format,
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
