package editbridge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testOrigin = "https://getresound.app"

func newTestBridge(t *testing.T, opts Options) (*Bridge, string, int) {
	t.Helper()
	opts.Origins = []string{testOrigin}
	b := New(opts)
	src := filepath.Join(t.TempDir(), "Funny clip.mp4")
	if err := os.WriteFile(src, []byte("not really a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	token, port, err := b.Add(src, filepath.Join(t.TempDir(), "Memes"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b, token, port
}

// do sends a request to the bridge's handler as the page would.
func do(b *Bridge, port int, method, path, origin, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:" + itoa(port)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	b.handler(port).ServeHTTP(rec, req)
	return rec
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestInfoAndSource(t *testing.T) {
	opened := make(chan string, 1)
	b, token, port := newTestBridge(t, Options{OnOpen: func(tk string) { opened <- tk }})

	rec := do(b, port, "GET", "/v1/"+token, testOrigin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("info: %d %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	var info struct {
		Filename, Folder, Type, Format string
		Size                           int64
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Filename != "Funny clip.mp4" || info.Folder != "Memes" || info.Type != "video/mp4" || info.Format != "flac" || info.Size != 18 {
		t.Errorf("info = %+v", info)
	}
	select {
	case tk := <-opened:
		if tk != token {
			t.Errorf("OnOpen(%q)", tk)
		}
	default:
		t.Error("OnOpen not called")
	}

	rec = do(b, port, "GET", "/v1/"+token+"/source", testOrigin, "")
	if rec.Code != http.StatusOK || rec.Body.String() != "not really a video" {
		t.Fatalf("source: %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// Only the app's own page, at this server's address, with a known token
// gets anything.
func TestRefusals(t *testing.T) {
	b, token, port := newTestBridge(t, Options{})
	for _, tc := range []struct {
		name, origin, host, path string
		want                     int
	}{
		{"no origin", "", "", "/v1/" + token, http.StatusForbidden},
		{"other origin", "https://evil.example", "", "/v1/" + token, http.StatusForbidden},
		{"rebound host", testOrigin, "evil.example:" + itoa(port), "/v1/" + token, http.StatusForbidden},
		{"unknown token", testOrigin, "", "/v1/0123456789abcdef0123456789abcdef/source", http.StatusNotFound},
		{"no token", testOrigin, "", "/", http.StatusNotFound},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Host = "127.0.0.1:" + itoa(port)
		if tc.host != "" {
			req.Host = tc.host
		}
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		b.handler(port).ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, rec.Code, tc.want)
		}
		if tc.want == http.StatusForbidden && rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: CORS headers on a refusal", tc.name)
		}
	}
}

func TestPreflight(t *testing.T) {
	b, token, port := newTestBridge(t, Options{})
	req := httptest.NewRequest("OPTIONS", "/v1/"+token+"/result", nil)
	req.Host = "127.0.0.1:" + itoa(port)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()
	b.handler(port).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: %d", rec.Code)
	}
	for k, v := range map[string]string{
		"Access-Control-Allow-Origin":          testOrigin,
		"Access-Control-Allow-Headers":         "Content-Type",
		"Access-Control-Allow-Private-Network": "true",
	} {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestResult(t *testing.T) {
	var got string
	b, token, port := newTestBridge(t, Options{OnResult: func(j Job, body io.Reader) (string, error) {
		if j.Token == "" || filepath.Base(j.Dir) != "Memes" {
			t.Errorf("OnResult got %+v", j)
		}
		data, _ := io.ReadAll(body)
		got = string(data)
		return "Funny clip (2)", nil
	}})
	rec := do(b, port, "POST", "/v1/"+token+"/result", testOrigin, "mp3 bytes")
	if rec.Code != http.StatusOK || got != "mp3 bytes" || !strings.Contains(rec.Body.String(), `"Funny clip (2)"`) {
		t.Fatalf("result: %d %s (got %q)", rec.Code, rec.Body, got)
	}

	b2, token2, port2 := newTestBridge(t, Options{OnResult: func(Job, io.Reader) (string, error) {
		return "", errString("This isn't a sound Resound can play.")
	}})
	rec = do(b2, port2, "POST", "/v1/"+token2+"/result", testOrigin, "junk")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "can play") {
		t.Fatalf("failed result: %d %s", rec.Code, rec.Body)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// A page that goes away and doesn't come back closes its file; one that
// reloads (asks for the file again in time) keeps it.
func TestClose(t *testing.T) {
	closed := make(chan string, 1)
	b, token, port := newTestBridge(t, Options{OnClose: func(tk string) { closed <- tk }})

	do(b, port, "POST", "/v1/"+token+"/close", testOrigin, "")
	do(b, port, "GET", "/v1/"+token, testOrigin, "") // reloaded
	b.mu.Lock()
	pending := b.jobs[token].closer != nil
	b.mu.Unlock()
	if pending {
		t.Fatal("a reload left the close pending")
	}

	b.closingAfter(token, 10*time.Millisecond)
	select {
	case tk := <-closed:
		if tk != token {
			t.Errorf("OnClose(%q)", tk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnClose not called")
	}
	if rec := do(b, port, "GET", "/v1/"+token, testOrigin, ""); rec.Code != http.StatusNotFound {
		t.Errorf("closed file still served: %d", rec.Code)
	}
}

// The server listens on loopback only while files are being edited.
func TestServerLifetime(t *testing.T) {
	b, token, port := newTestBridge(t, Options{})
	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/v1/" + token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden { // no Origin
		t.Errorf("status %d", resp.StatusCode)
	}
	b.Remove(token)
	if _, err := http.Get("http://127.0.0.1:" + itoa(port) + "/"); err == nil {
		t.Error("server still running without files")
	}
}

func TestPageURL(t *testing.T) {
	got := PageURL("https://getresound.app/", 51234, "abc")
	if got != "https://getresound.app/edit#v=1&p=51234&t=abc" {
		t.Errorf("PageURL = %q", got)
	}
}

func TestEditable(t *testing.T) {
	for name, want := range map[string]bool{"a.MP4": true, "b.mkv": true, "c.m4a": true, "d.mp3": true, "e.txt": false, "f": false} {
		if Editable(name) != want {
			t.Errorf("Editable(%q) = %v", name, !want)
		}
	}
}
