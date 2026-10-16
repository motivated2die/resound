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
// picker's default data URL is overridden by data-source). Links the user
// follows (<a href="https://…">) are fine: they load nothing.
func TestWebSelfContained(t *testing.T) {
	url := regexp.MustCompile(`(https?:)?//[a-z0-9-]+(\.[a-z0-9-]+)+[/"'\s)]`)
	link := regexp.MustCompile(`<a [^>]*href="https://[^"]*"`)
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
			if strings.HasSuffix(path, ".html") {
				line = link.ReplaceAllString(line, "<a")
			}
			if m := url.FindString(line); m != "" && !strings.Contains(line, "www.w3.org/2000/svg") {
				t.Errorf("%s:%d references %q", path, i+1, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	app, _ := fs.ReadFile(src, "app.html")
	if !strings.Contains(string(app), `data-source="vendor/emoji-picker-element-data/data.json"`) {
		t.Error("the emoji picker has no local data-source and would load its data from a CDN")
	}
	if _, err := fs.Stat(src, "vendor/emoji-picker-element-data/data.json"); err != nil {
		t.Error(err)
	}
	// rtc.js forwards pairing links and carries the remote's requests, so it
	// must run before any other script of a page.
	for _, page := range []string{"index.html", "app.html", "connect.html"} {
		data, _ := fs.ReadFile(src, page)
		first := regexp.MustCompile(`<script[^>]*>`).FindString(string(data))
		if !strings.Contains(first, `src="js/rtc.js"`) {
			t.Errorf("%s must load js/rtc.js as its first script, not %q", page, first)
		}
	}
}

// Every icon a page uses has its SVG and its CSS rule in that page's
// stylesheet.
func TestWebIcons(t *testing.T) {
	pages := []struct {
		css   string
		files []string
	}{
		{"css/styles.css", []string{"app.html", "js/app.js", "js/editMode.js", "js/layout.js", "js/sound-settings.js"}},
		{"css/landing.css", []string{"index.html"}},
	}
	attr := regexp.MustCompile(`data-icon="([a-z_-]+)"`)
	quoted := regexp.MustCompile(`'([a-z_-]+)'`)
	assign := regexp.MustCompile(`(?:dataset\.icon|iconName) = (.*)`) // the value side only, not e.g. a case label
	for _, page := range pages {
		css, err := fs.ReadFile(src, page.css)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]string{}
		for _, path := range page.files {
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
		if len(names) < 10 {
			t.Fatalf("%s: found only %d icon names: %v", page.css, len(names), names)
		}
		for name, path := range names {
			if _, err := fs.Stat(src, "assets/icons/"+name+".svg"); err != nil {
				t.Errorf("%s uses icon %q, but assets/icons/%s.svg is missing", path, name, name)
			}
			if !strings.Contains(string(css), `.icon[data-icon="`+name+`"]`) {
				t.Errorf("%s uses icon %q, but %s has no rule for it", path, name, page.css)
			}
		}
	}
}

// The build fills in the service worker, which lists every page and script
// of the app, and adds the help center, which it must not list.
func TestBuild(t *testing.T) {
	out := t.TempDir()
	if err := build(filepath.Join("..", "..", "webapp", "src"), filepath.Join("..", "..", "webapp", "help"), out); err != nil {
		t.Fatal(err)
	}
	sw, err := os.ReadFile(filepath.Join(out, "sw.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"/"`, `"/app"`, `"/connect"`, `"/js/rtc.js"`, `"/manifest.json"`} {
		if !strings.Contains(string(sw), want) {
			t.Errorf("sw.js does not cache %s", want)
		}
	}
	if strings.Contains(string(sw), "__FILES__") || strings.Contains(string(sw), "__VERSION__") || strings.Contains(string(sw), "README") {
		t.Errorf("sw.js not filled in properly")
	}
	if strings.Contains(string(sw), `"/help`) {
		t.Errorf("sw.js caches the help center")
	}
	if !strings.Contains(string(sw), `url.pathname.startsWith('/help/')`) {
		t.Errorf("sw.js doesn't leave /help/ to the network")
	}
	for _, page := range []string{"help/index.html", "help/voice-chat.html", "help/help.css", "help/help.js", "help/search.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(page))); err != nil {
			t.Errorf("help center: %v", err)
		}
	}
}
