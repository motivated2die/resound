package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var src = os.DirFS(filepath.Join("..", "..", "webapp", "src"))

// The app must not load anything from the internet: no URLs in our own
// files (vendored libraries only mention them in comments, and the emoji
// picker's default data URL is overridden by data-source).
func TestWebSelfContained(t *testing.T) {
	url := regexp.MustCompile(`(https?:)?//[a-z0-9-]+(\.[a-z0-9-]+)+[/"'\s)]`)
	err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(path, "vendor/") {
			return err
		}
		if !strings.HasSuffix(path, ".html") && !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".css") {
			return nil
		}
		data, err := fs.ReadFile(src, path)
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
	index, _ := fs.ReadFile(src, "index.html")
	if !strings.Contains(string(index), `data-source="vendor/emoji-picker-element-data/data.json"`) {
		t.Error("the emoji picker has no local data-source and would load its data from a CDN")
	}
	if _, err := fs.Stat(src, "vendor/emoji-picker-element-data/data.json"); err != nil {
		t.Error(err)
	}
	// The connection must be set up before the remote's own scripts run.
	if i, j := strings.Index(string(index), `src="js/rtc.js"`), strings.Index(string(index), "<script"); i < 0 || j < i-len(`<script `) {
		t.Error("index.html must load js/rtc.js as its first script")
	}
}

// Every icon the page uses has its SVG and its CSS rule.
func TestWebIcons(t *testing.T) {
	css, err := fs.ReadFile(src, "css/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	attr := regexp.MustCompile(`data-icon="([a-z_-]+)"`)
	quoted := regexp.MustCompile(`'([a-z_-]+)'`)
	assign := regexp.MustCompile(`(?:dataset\.icon|iconName) = (.*)`) // the value side only, not e.g. a case label
	for _, path := range []string{"index.html", "js/app.js", "js/editMode.js", "js/layout.js", "js/sound-settings.js"} {
		data, err := fs.ReadFile(src, path)
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
		if _, err := fs.Stat(src, "assets/icons/"+name+".svg"); err != nil {
			t.Errorf("%s uses icon %q, but assets/icons/%s.svg is missing", path, name, name)
		}
		if !strings.Contains(string(css), `.icon[data-icon="`+name+`"]`) {
			t.Errorf("%s uses icon %q, but styles.css has no rule for it", path, name)
		}
	}
}

// The build fills in the service worker, which lists every page and script.
func TestBuild(t *testing.T) {
	out := t.TempDir()
	if err := build(filepath.Join("..", "..", "webapp", "src"), out); err != nil {
		t.Fatal(err)
	}
	sw, err := os.ReadFile(filepath.Join(out, "sw.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"/index.html"`, `"/connect.html"`, `"/js/rtc.js"`, `"/manifest.json"`} {
		if !strings.Contains(string(sw), want) {
			t.Errorf("sw.js does not cache %s", want)
		}
	}
	if strings.Contains(string(sw), "__FILES__") || strings.Contains(string(sw), "__VERSION__") || strings.Contains(string(sw), "README") {
		t.Errorf("sw.js not filled in properly")
	}
}
