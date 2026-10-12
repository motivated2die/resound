package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"0.8.0", "0.7.0", true},
		{"0.8.0", "0.8.0", false},
		{"0.7.9", "0.8.0", false},
		{"0.10.0", "0.9.0", true}, // numeric, not lexical
		{"1.0.0", "0.99.99", true},
		{"0.8.1", "0.8.0", true},
		{"0.8.0", "0.8.0-dev", true}, // release beats dev build of the same version
		{"0.8.0", "0.8.0-rc.2", true},
		{"0.8.0", "0.9.0-dev", false},
		{"0.9.0-rc.1", "0.8.0", false}, // pre-releases are never offered
		{"0.8.0", "", true},            // unknown current version
		{"garbage", "0.1.0", false},
		{"0.8", "0.1.0", false},
	} {
		if got := Newer(tc.latest, tc.current); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestParseSums(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{sum + "  Resound.exe\n", sum, true},
		{sum + " *Resound.exe\r\n", sum, true},
		{strings.ToUpper(sum) + "  Resound.exe", sum, true},
		{sum + "  Other.exe\n", "", false},
		{"abcd  Resound.exe\n", "", false}, // too short
		{"", "", false},
	} {
		got, ok := parseSums(strings.NewReader(tc.in), AssetName)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseSums(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCheckAPI(t *testing.T) {
	for api, ok := range map[string]bool{
		"https://example.com/repos/a/b": true,
		"http://127.0.0.1:8090":         true,
		"http://localhost:8090":         true,
		"http://[::1]:8090":             true,
		"http://example.com":            false,
		"ftp://127.0.0.1":               false,
		"https://":                      false,
	} {
		if err := CheckAPI(api); (err == nil) != ok {
			t.Errorf("CheckAPI(%q) = %v, want ok=%v", api, err, ok)
		}
	}
}

func TestAllowedHosts(t *testing.T) {
	c := Client{}
	for raw, want := range map[string]bool{
		"https://api.github.com/repos/x":                          true,
		"https://github.com/motivated2die/resound/releases/x":     true,
		"https://objects.githubusercontent.com/x":                 true,
		"https://release-assets.githubusercontent.com/x":          true,
		"http://github.com/x":                                     false,
		"https://evil.example/x":                                  false,
		"https://githubusercontent.com.evil.example/x":            false,
		"https://notgithub.com/x":                                 false,
		"https://objects.githubusercontent.com.evil.example/x":    false,
		"https://github.com.evil.example/motivated2die/resound/x": false,
	} {
		u, _ := url.Parse(raw)
		if got := c.allowed(u); got != want {
			t.Errorf("allowed(%s) = %v, want %v", raw, got, want)
		}
	}
}

// fakeGitHub serves /releases/latest and the two assets like GitHub does.
type fakeGitHub struct {
	status     int    // for /releases/latest; 0 = 200
	tag        string // e.g. "v0.8.0"
	prerelease bool
	exe        []byte
	sums       string // SHA256SUMS.txt; "" = computed from exe
	noAssets   bool
	assetHost  string // overrides the asset URLs' host
}

func (f *fakeGitHub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/releases/latest":
			if f.status != 0 {
				w.WriteHeader(f.status)
				return
			}
			host := srv.URL
			if f.assetHost != "" {
				host = f.assetHost
			}
			type asset struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
				Size int    `json:"size"`
			}
			assets := []asset{
				{AssetName, host + "/download/" + AssetName, len(f.exe)},
				{SumsName, host + "/download/" + SumsName, 0},
			}
			if f.noAssets {
				assets = nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": f.tag, "html_url": srv.URL + "/tag/" + f.tag,
				"draft": false, "prerelease": f.prerelease, "assets": assets,
			})
		case "/download/" + AssetName:
			_, _ = w.Write(f.exe)
		case "/download/" + SumsName:
			sums := f.sums
			if sums == "" {
				h := sha256.Sum256(f.exe)
				sums = hex.EncodeToString(h[:]) + "  " + AssetName + "\n"
			}
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testClient(srv *httptest.Server) Client {
	return Client{API: srv.URL, UserAgent: "Resound-test"}
}

func TestLatestAndDownload(t *testing.T) {
	exe := bytes.Repeat([]byte("MZ resound "), 50_000)
	srv := (&fakeGitHub{tag: "v0.8.0", exe: exe}).serve(t)
	c := testClient(srv)

	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.8.0" || !strings.HasSuffix(rel.Page, "/tag/v0.8.0") {
		t.Fatalf("release = %+v", rel)
	}

	dir := t.TempDir()
	// A leftover from an earlier download is replaced.
	if err := os.WriteFile(filepath.Join(dir, AssetName), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	var last, total int64
	path, err := c.Download(context.Background(), rel, dir, func(d, t int64) { last, total = d, t })
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, exe) {
		t.Fatalf("downloaded file differs (err %v)", err)
	}
	if last != int64(len(exe)) || total != int64(len(exe)) {
		t.Errorf("progress ended at %d/%d, want %d", last, total, len(exe))
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Errorf("the .part file was left behind")
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	srv := (&fakeGitHub{tag: "v0.8.0", exe: []byte("MZ tampered"),
		sums: strings.Repeat("00", 32) + "  Resound.exe\n"}).serve(t)
	c := testClient(srv)
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := c.Download(context.Background(), rel, dir, nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Download = %v, want a checksum error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files left after a failed download: %v", entries)
	}
}

func TestLatestNoRelease(t *testing.T) {
	for name, f := range map[string]*fakeGitHub{
		"404":        {status: http.StatusNotFound},
		"prerelease": {tag: "v0.9.0-rc.1", prerelease: true},
		"rc tag":     {tag: "v0.9.0-rc.1"},
		"no assets":  {tag: "v0.8.0", noAssets: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testClient(f.serve(t)).Latest(context.Background())
			if !errors.Is(err, ErrNoRelease) {
				t.Fatalf("Latest = %v, want ErrNoRelease", err)
			}
		})
	}
}

func TestLatestErrors(t *testing.T) {
	for name, f := range map[string]*fakeGitHub{
		"rate limit": {status: http.StatusForbidden},
		"server":     {status: http.StatusInternalServerError},
		"bad tag":    {tag: "release-8"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testClient(f.serve(t)).Latest(context.Background())
			if err == nil || errors.Is(err, ErrNoRelease) {
				t.Fatalf("Latest = %v, want an error", err)
			}
		})
	}
}

// Asset URLs that point away from the API's host are refused.
func TestDownloadForeignHost(t *testing.T) {
	srv := (&fakeGitHub{tag: "v0.8.0", exe: []byte("MZ"), assetHost: "http://127.0.0.2:1"}).serve(t)
	c := testClient(srv)
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Download(context.Background(), rel, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected download host") {
		t.Fatalf("Download = %v, want a host error", err)
	}
}

// Redirects are followed only to allowed hosts.
func TestDownloadRedirectAway(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("MZ evil"))
	}))
	defer other.Close()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.8.0","assets":[{"name":"Resound.exe","browser_download_url":"%[1]s/exe"},{"name":"SHA256SUMS.txt","browser_download_url":"%[1]s/sums"}]}`, srv.URL)
		default:
			http.Redirect(w, r, other.URL+"/x", http.StatusFound)
		}
	}))
	defer srv.Close()
	c := testClient(srv)
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Download(context.Background(), rel, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "unexpected host") {
		t.Fatalf("Download = %v, want a redirect error", err)
	}
}
