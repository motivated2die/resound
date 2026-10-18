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
// follows (<a href="https://…">) are fine: they load nothing. The one
// exception is the editor the edit page embeds (TestEditPagePrivate).
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
			if m := url.FindString(line); m != "" && !strings.Contains(line, "www.w3.org/2000/svg") &&
				!(editorFiles[path] && strings.HasPrefix(m, editorOrigin)) {
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

// The editor the edit page embeds, and the edit page's files, the only ones
// that may name it.
const editorOrigin = "https://audiocutter.online"

var editorFiles = map[string]bool{"edit.html": true, "js/edit.js": true}

// The edit page hands the user's files between Resound (on 127.0.0.1) and
// the editor. Its host must learn nothing about them: the page's policy
// lets it reach Resound and frame the editor only, never its own host,
// and the site-wide policy (which would apply on top) is dropped for it.
func TestEditPagePrivate(t *testing.T) {
	page, err := fs.ReadFile(src, "edit.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`<meta http-equiv="Content-Security-Policy" content="([^"]*)">`).FindSubmatch(page)
	if m == nil {
		t.Fatal("edit.html has no Content-Security-Policy <meta> tag")
	}
	directives := map[string]string{}
	for _, d := range strings.Split(string(m[1]), ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(d), " ")
		directives[name] = value
	}
	for name, want := range map[string]string{
		"default-src": "'none'",
		"connect-src": "http://127.0.0.1:*",
		"frame-src":   editorOrigin,
		"form-action": "'none'",
	} {
		if got := directives[name]; got != want {
			t.Errorf("edit.html: %s is %q, want %q", name, got, want)
		}
	}
	if strings.Contains(string(page), `src="js/rtc.js"`) || strings.Contains(string(page), `rel="manifest"`) {
		t.Error("edit.html must not load the remote's connection or the app manifest")
	}

	headers, err := fs.ReadFile(src, "_headers")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/edit", "/edit.html"} {
		block := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(path) + `
((?:[ 	]+.*
?)*)`).FindSubmatch(headers)
		if block == nil || !strings.Contains(string(block[1]), "! Content-Security-Policy") {
			t.Errorf("_headers must drop the site-wide Content-Security-Policy for %s", path)
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

// TestCardColors checks that every colour the sound sheet offers
// (app.html) has its card, playing highlight, fill and swatch styles, and is
// offered by the search's colour filter too. internal/ui's
// TestOverlayColorsMatchApp checks the in-game overlay knows them.
func TestCardColors(t *testing.T) {
	read := func(path string) string {
		data, err := fs.ReadFile(src, path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	var sheet []string
	for _, m := range regexp.MustCompile(`data-color="([a-z]+)"`).FindAllStringSubmatch(read("app.html"), -1) {
		sheet = append(sheet, m[1])
	}
	if len(sheet) != 12 || sheet[0] != "default" {
		t.Fatalf("sheet colours = %v", sheet)
	}
	m := regexp.MustCompile(`const COLORS = \[([^\]]*)\]`).FindStringSubmatch(read("js/search.js"))
	if m == nil {
		t.Fatal("search.js: no COLORS list")
	}
	css := read("css/styles.css")
	for _, c := range sheet {
		if !strings.Contains(m[1], "'"+c+"'") {
			t.Errorf("search.js: the colour filter lacks %q", c)
		}
		if !strings.Contains(css, ".color-swatch."+c+" {") {
			t.Errorf("styles.css: no swatch for %q", c)
		}
		if c == "default" {
			continue
		}
		for _, want := range []string{"--sound-color-" + c + "-bg:", "--sound-color-" + c + "-progress:", "--sound-color-" + c + "-highlight:",
			".sound-card.sound-color-" + c + " {", ".sound-card.sound-color-" + c + ".playing {"} {
			if !strings.Contains(css, want) {
				t.Errorf("styles.css: %q is missing for colour %q", want, c)
			}
		}
	}
}
