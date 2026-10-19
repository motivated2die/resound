// Command webapp builds the Resound app: the static website on HTTPS
// (getresound.app, on Cloudflare Pages) that phones install and that connects
// to the PC over WebRTC. It copies webapp/src and fills in the service
// worker's version and file list, then renders the help center
// (webapp/help, see help.go) into /help/, which the app doesn't cache, and
// lists the pages for search engines in sitemap.xml.
//
//	go run ./cmd/webapp                  build into webapp/dist
//	go run ./cmd/webapp -serve :8095     build, then serve it on localhost
//
// Nothing here is embedded into Resound.exe.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// site is where the website is deployed: canonical URLs, link previews and
// the sitemap name it (robots.txt and index.html too, by hand).
const site = "https://getresound.app"

// sitemapPages are the pages of webapp/src for search engines; the app's
// own pages (/app, /connect, /edit) are left out and marked noindex.
var sitemapPages = []string{"index.html"}

func main() {
	src := flag.String("src", "webapp/src", "the app's files")
	help := flag.String("help", "webapp/help", "the help center's articles")
	out := flag.String("out", "webapp/dist", "output folder (replaced)")
	serve := flag.String("serve", "", "serve the result on this address, e.g. :8095 (localhost is a secure context)")
	flag.Parse()

	if err := build(*src, *help, *out); err != nil {
		log.Fatal(err)
	}
	if *serve == "" {
		return
	}
	addr := *serve
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	fmt.Printf("serving %s on http://%s\n", *out, addr)
	files := shortURLs(*out, http.FileServer(http.Dir(*out)))
	log.Fatal(http.ListenAndServe(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})))
}

func build(src, helpDir, out string) error {
	files, err := readFiles(os.DirFS(src))
	if err != nil {
		return fmt.Errorf("%s: run from the repository root (%w)", src, err)
	}
	sw, ok := files["sw.js"]
	if !ok || !strings.Contains(string(sw), "__FILES__") {
		return errors.New("sw.js is missing or has no __FILES__ placeholder")
	}
	version, list := precache(files)
	text := strings.Replace(string(sw), "'__VERSION__'", "'"+version+"'", 1)
	files["sw.js"] = []byte(strings.Replace(text, "__FILES__", list, 1))

	// The help center comes after the file list: the installed app doesn't
	// cache it, and a change to it doesn't make a new app version.
	help, err := buildHelp(os.DirFS(helpDir))
	if err != nil {
		return fmt.Errorf("help center (%s): %w", helpDir, err)
	}
	for name, data := range help {
		if _, ok := files[name]; ok {
			return fmt.Errorf("help center: %s exists in %s too", name, src)
		}
		files[name] = data
	}
	files["sitemap.xml"] = sitemap(files)

	if err := os.RemoveAll(out); err != nil {
		return err
	}
	for name, data := range files {
		p := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("built %s: %d files, version %s\n", out, len(files), version)
	return nil
}

func readFiles(fsys fs.FS) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, name)
		files[name] = data
		return err
	})
	return files, err
}

// precache returns the service worker's version, which changes with any
// file, and the list of URLs it caches: everything the app can load. Pages
// are listed by their short URLs (/, /app), which every host serves.
func precache(files map[string][]byte) (version, list string) {
	var names []string
	for name := range files {
		base := path.Base(name)
		if name == "sw.js" || name == "_headers" || name == "robots.txt" || base == "README.md" || strings.HasPrefix(base, "LICENSE") {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	sum := sha256.New()
	var urls []string
	for _, name := range names {
		sum.Write([]byte(name))
		sum.Write(files[name])
		urls = append(urls, pageURL(name))
	}
	data, _ := json.Marshal(urls)
	return hex.EncodeToString(sum.Sum(nil))[:12], string(data)
}

// pageURL is a file's URL: pages without .html (index.html is /), as
// Cloudflare Pages serves them, everything else as is.
func pageURL(name string) string {
	switch {
	case name == "index.html":
		return "/"
	case strings.HasSuffix(name, ".html") && !strings.Contains(name, "/"):
		return "/" + strings.TrimSuffix(name, ".html")
	}
	return "/" + name
}

// sitemap lists the landing page and every help center page, by their
// canonical URLs.
func sitemap(files map[string][]byte) []byte {
	var urls []string
	for _, name := range sitemapPages {
		urls = append(urls, site+pageURL(name))
	}
	var help []string
	for name := range files {
		if slug, ok := strings.CutPrefix(name, "help/"); ok && strings.HasSuffix(slug, ".html") {
			help = append(help, site+helpPath(strings.TrimSuffix(slug, ".html")))
		}
	}
	slices.Sort(help)
	urls = append(urls, help...)

	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, u := range urls {
		fmt.Fprintf(&b, "  <url><loc>%s</loc></url>\n", u)
	}
	b.WriteString("</urlset>\n")
	return []byte(b.String())
}

// shortURLs serves /app from app.html, like the hosts do.
func shortURLs(dir string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/" && path.Ext(p) == "" {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)+".html")); err == nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = p + ".html"
				next.ServeHTTP(w, r2)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
