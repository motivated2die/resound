package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// Web files get a content ETag, so browsers (and the emoji picker) can
// revalidate them with a cheap 304.
func TestWebETag(t *testing.T) {
	s, _, _ := newTestServer(t)
	s.web = fstest.MapFS{
		"index.html":            {Data: []byte("app")},
		"assets/icons/edit.svg": {Data: []byte("<svg/>")},
	}
	h := s.Handler()

	rec := do(t, h, "GET", "/assets/icons/edit.svg", "", nil)
	tag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || !strings.HasPrefix(tag, `"`) {
		t.Fatalf("GET = %d, ETag %q", rec.Code, tag)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}

	req := httptest.NewRequest("GET", "/assets/icons/edit.svg", nil)
	req.Header.Set("If-None-Match", tag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("revalidation = %d, want 304", rec.Code)
	}

	rec = do(t, h, "HEAD", "/assets/icons/edit.svg", "", nil)
	if rec.Header().Get("ETag") != tag {
		t.Errorf("HEAD ETag = %q, want %q", rec.Header().Get("ETag"), tag)
	}
}

// The remote must not load anything from the internet: no URLs in our own
// web files (vendored libraries only mention them in comments, and the
// emoji picker's default data URL is overridden by data-source).
func TestWebSelfContained(t *testing.T) {
	web := WebFS()
	url := regexp.MustCompile(`(https?:)?//[a-z0-9-]+(\.[a-z0-9-]+)+[/"'\s)]`)
	err := fs.WalkDir(web, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(path, "vendor/") {
			return err
		}
		if !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".css") {
			return nil
		}
		data, err := fs.ReadFile(web, path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if m := url.FindString(line); m != "" && !strings.Contains(line, "www.w3.org/2000/svg") {
				t.Errorf("%s:%d references %q", path, i+1, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	index, _ := fs.ReadFile(web, "index.html")
	if !strings.Contains(string(index), `data-source="vendor/emoji-picker-element-data/data.json"`) {
		t.Error("the emoji picker has no local data-source and would load its data from a CDN")
	}
	if _, err := fs.Stat(web, "vendor/emoji-picker-element-data/data.json"); err != nil {
		t.Error(err)
	}
}

// Every icon the page uses has its SVG and its CSS rule.
func TestWebIcons(t *testing.T) {
	web := WebFS()
	css, err := fs.ReadFile(web, "css/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	attr := regexp.MustCompile(`data-icon="([a-z_-]+)"`)
	quoted := regexp.MustCompile(`'([a-z_-]+)'`)
	assign := regexp.MustCompile(`(?:dataset\.icon|iconName) = (.*)`) // the value side only, not e.g. a case label
	for _, path := range []string{"index.html", "js/app.js", "js/editMode.js", "js/layout.js", "js/screen-wake.js", "js/sound-settings.js"} {
		data, err := fs.ReadFile(web, path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			for _, m := range attr.FindAllStringSubmatch(line, -1) {
				names[m[1]] = path
			}
			if m := assign.FindStringSubmatch(line); m != nil {
				for _, m := range quoted.FindAllStringSubmatch(m[1], -1) {
					if m[1] != "favorites" { // a tab ID, not an icon
						names[m[1]] = path
					}
				}
			}
		}
	}
	if len(names) < 20 {
		t.Fatalf("found only %d icon names: %v", len(names), names)
	}
	for name, path := range names {
		if _, err := fs.Stat(web, "assets/icons/"+name+".svg"); err != nil {
			t.Errorf("%s uses icon %q, but assets/icons/%s.svg is missing", path, name, name)
		}
		if !strings.Contains(string(css), `.icon[data-icon="`+name+`"]`) {
			t.Errorf("%s uses icon %q, but styles.css has no rule for it", path, name)
		}
	}
}
