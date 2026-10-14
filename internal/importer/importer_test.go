package importer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var exts = []string{".mp3", ".wav", ".flac", ".ogg"}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestParsePageMyinstants(t *testing.T) {
	f, err := os.Open("testdata/myinstants-instant.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := ParsePage(f, mustURL(t, "https://www.myinstants.com/en/instant/fahhhhhhhhhhhhhh-3525/"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://www.myinstants.com/media/sounds/fahhhhhhhhhhhhhh.mp3"; got.AudioURL.String() != want {
		t.Errorf("audio = %s, want %s", got.AudioURL, want)
	}
	if got.Name != "FAHHHHHHHHHHHHHH" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestParsePageSearchResults(t *testing.T) {
	f, err := os.Open("testdata/myinstants-search.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Listing pages play sounds from onclick handlers only: nothing to take.
	if _, err := ParsePage(f, mustURL(t, "https://www.myinstants.com/en/search/?name=vine")); !errors.Is(err, ErrNoSound) {
		t.Fatalf("err = %v, want ErrNoSound", err)
	}
}

func TestParsePageGeneric(t *testing.T) {
	base := mustURL(t, "https://example.org/sounds/page.html")
	for _, tc := range []struct {
		name, page, audio, title string
		err                      error
	}{
		{"audio element", `<title>Ding | Example</title><audio src="ding.ogg"></audio>`, "https://example.org/sounds/ding.ogg", "Ding", nil},
		{"source element and same link", `<audio><source src="/a/b.wav"></audio><a href="/a/b.wav">get</a>`, "https://example.org/a/b.wav", "b", nil},
		{"og title", `<meta property="og:title" content="Big Horn - Sound Button"><a href="horn.mp3">x</a>`, "https://example.org/sounds/horn.mp3", "Big Horn", nil},
		{"base href", `<base href="https://cdn.example.net/x/"><a href="y.mp3">y</a>`, "https://cdn.example.net/x/y.mp3", "y", nil},
		{"several", `<a href="a.mp3">a</a><a href="b.mp3">b</a>`, "", "", ErrManySounds},
		{"none", `<p>Hello</p><a href="next.html">next</a>`, "", "", ErrNoSound},
		{"no web link", `<meta property="og:audio" content="file:///C:/x.mp3">`, "", "", ErrNoSound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePage(strings.NewReader(tc.page), base)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.AudioURL.String() != tc.audio || got.Name != tc.title {
				t.Errorf("got %s %q, want %s %q", got.AudioURL, got.Name, tc.audio, tc.title)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	long := strings.Repeat("ä", MaxNameLen+20)
	for in, want := range map[string]string{
		"FAHHHHHHHHHHHHHH":          "FAHHHHHHHHHHHHHH",
		`What? Da "dog" <doin>`:     "What Da dog doin",
		"  spaced\t\nout  name  ":   "spaced out name",
		"a/b\\c:d|e*f":              "abcdef",
		"trailing dots...":          "trailing dots",
		"zero\u200bwidth\u202eflip": "zerowidthflip",
		"bell\x07":                  "bell",
		"CON":                       "CON_",
		"com1":                      "com1_",
		"LPT9.mp3":                  "LPT9.mp3_",
		"Console":                   "Console",
		"":                          "Sound",
		"???":                       "Sound",
		"emoji 💀 ok":                "emoji 💀 ok",
		long:                        strings.Repeat("ä", MaxNameLen),
	} {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLink(t *testing.T) {
	for in, ok := range map[string]bool{
		"https://www.myinstants.com/en/instant/vine-boom-sound-70972/": true,
		"  http://example.org/a.mp3\r\n":                               true,
		"ftp://example.org/a.mp3":                                      false,
		"file:///C:/sounds/a.mp3":                                      false,
		"two https://a.b https://c.d":                                  false,
		"not a link":                                                   false,
		"https://":                                                     false,
	} {
		if _, got := ParseLink(in); got != ok {
			t.Errorf("ParseLink(%q) = %v, want %v", in, got, ok)
		}
	}
}

func TestPublicOnly(t *testing.T) {
	for addr, ok := range map[string]bool{
		"151.101.1.1:443":    true,
		"[2606:4700::1]:443": true,
		"127.0.0.1:80":       false,
		"192.168.1.20:8080":  false,
		"10.0.0.1:443":       false,
		"172.16.5.5:443":     false,
		"169.254.169.254:80": false,
		"100.64.1.1:443":     false,
		"0.0.0.0:80":         false,
		"[::1]:80":           false,
		"[fe80::1]:80":       false,
		"[fd00::1]:80":       false,
	} {
		if err := publicOnly("tcp", addr, nil); (err == nil) != ok {
			t.Errorf("publicOnly(%s) = %v, want ok=%v", addr, err, ok)
		}
	}
}

// The guarded client refuses this PC before sending anything.
func TestFetchRefusesLocalAddresses(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	f := New(exts, nil)
	_, err := f.Fetch(context.Background(), srv.URL+"/a.mp3", t.TempDir())
	if !errors.Is(err, ErrBlocked) || hit {
		t.Fatalf("err = %v, hit = %v; want ErrBlocked without a request", err, hit)
	}
}

// fakeMP3 is enough bytes to pass for a file; the check is faked.
var fakeMP3 = []byte("ID3\x03\x00\x00\x00\x00\x00\x00" + strings.Repeat("\xff\xfb\x90\x64", 64))

func testServer(t *testing.T) *httptest.Server {
	page, err := os.ReadFile("testdata/myinstants-instant.html")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/en/instant/fahhhhhhhhhhhhhh-3525/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The fixture's absolute og:audio points at Myinstants; serve it here.
		w.Write([]byte(strings.ReplaceAll(string(page), "https://www.myinstants.com", "http://"+r.Host)))
	})
	mux.HandleFunc("/media/sounds/fahhhhhhhhhhhhhh.mp3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(fakeMP3)
	})
	mux.HandleFunc("/direct/vine-boom_2.mp3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(fakeMP3)
	})
	mux.HandleFunc("/m4a", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp4")
		w.Write(fakeMP3)
	})
	mux.HandleFunc("/broken.mp3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("<html>not a sound</html>"))
	})
	mux.HandleFunc("/plain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// testFetcher talks to the local test server (New's client would refuse
// it) and "decodes" by looking for the fake MP3's bytes.
func testFetcher(srv *httptest.Server) *Fetcher {
	return &Fetcher{Client: srv.Client(), Extensions: exts, Check: func(p string) error {
		data, err := os.ReadFile(p)
		if err != nil || !strings.HasPrefix(string(data), "ID3") {
			return errors.New("not audio")
		}
		if filepath.Ext(p) != ".mp3" {
			return errors.New("wrong extension")
		}
		return nil
	}}
}

func TestFetchMyinstantsPage(t *testing.T) {
	srv := testServer(t)
	dir := t.TempDir()
	f := testFetcher(srv)
	got, err := f.Fetch(context.Background(), srv.URL+"/en/instant/fahhhhhhhhhhhhhh-3525/", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "FAHHHHHHHHHHHHHH" || got.Path != filepath.Join(dir, "FAHHHHHHHHHHHHHH.mp3") {
		t.Fatalf("got %+v", got)
	}
	// A second import of the same sound gets its own name.
	again, err := f.Fetch(context.Background(), srv.URL+"/en/instant/fahhhhhhhhhhhhhh-3525/", dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "FAHHHHHHHHHHHHHH (2)" {
		t.Fatalf("second name = %q", again.Name)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("%d files in the folder, want 2 (no temporary files left)", len(entries))
	}
}

func TestFetchDirectLink(t *testing.T) {
	srv := testServer(t)
	dir := t.TempDir()
	got, err := testFetcher(srv).Fetch(context.Background(), srv.URL+"/direct/vine-boom_2.mp3", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "vine boom 2" {
		t.Fatalf("name = %q", got.Name)
	}
}

func TestFetchErrors(t *testing.T) {
	srv := testServer(t)
	for path, want := range map[string]error{
		"/m4a":        ErrUnsupported,
		"/broken.mp3": ErrUnsupported,
		"/plain":      ErrNoSound,
	} {
		dir := t.TempDir()
		_, err := testFetcher(srv).Fetch(context.Background(), srv.URL+path, dir)
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", path, err, want)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("%s: %d file(s) left behind", path, len(entries))
		}
	}
	if _, err := testFetcher(srv).Fetch(context.Background(), srv.URL+"/missing", t.TempDir()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing page: err = %v", err)
	}
}

func TestMoveUniqueNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "Horn.mp3")
	os.WriteFile(existing, []byte("old"), 0o644)
	src := filepath.Join(dir, "~tmp.mp3")
	os.WriteFile(src, []byte("new"), 0o644)
	dst, err := MoveUnique(src, dir, "Horn", ".mp3")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dst) != "Horn (2).mp3" {
		t.Fatalf("dst = %s", dst)
	}
	if data, _ := os.ReadFile(existing); string(data) != "old" {
		t.Fatal("existing file was replaced")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source still exists")
	}
}
