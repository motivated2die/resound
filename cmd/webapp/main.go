// Command webapp builds the installable app: the static web page hosted on
// HTTPS (Netlify) that phones install and that connects to the PC over
// WebRTC. It is the phone remote (internal/server/web/) with webapp/src/
// laid over it, the WebRTC transport (js/rtc.js) loaded first in index.html,
// and a service worker listing every file.
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
	"path/filepath"
	"slices"
	"strings"
)

// skipped files of the remote that the app doesn't use.
var skipped = []string{"README.md", "js/service-worker.js"}

func main() {
	remote := flag.String("remote", "internal/server/web", "the phone remote's files")
	overlay := flag.String("src", "webapp/src", "the app's own files, laid over the remote")
	out := flag.String("out", "webapp/dist", "output folder (replaced)")
	serve := flag.String("serve", "", "serve the result on this address, e.g. :8095 (localhost is a secure context)")
	flag.Parse()

	if err := build(*remote, *overlay, *out); err != nil {
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
	files := http.FileServer(http.Dir(*out))
	log.Fatal(http.ListenAndServe(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})))
}

func build(remote, overlay, out string) error {
	if _, err := os.Stat(filepath.Join(remote, "index.html")); err != nil {
		return fmt.Errorf("%s: run from the repository root (%w)", remote, err)
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	files := map[string][]byte{}
	for _, dir := range []string{remote, overlay} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if slices.Contains(skipped, rel) && dir == remote {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[rel] = data
			return nil
		})
		if err != nil {
			return err
		}
	}

	// The transport must run before the remote's scripts.
	index := string(files["index.html"])
	head := strings.Index(index, "<script")
	if head < 0 {
		return errors.New("index.html has no <script> to put js/rtc.js before")
	}
	files["index.html"] = []byte(index[:head] + "<script src=\"js/rtc.js\"></script>\n    " + index[head:])

	// The service worker caches every file; its version changes with them.
	var names []string
	for name := range files {
		if name != "sw.js" && name != "_headers" && !strings.HasSuffix(name, "README.md") && !strings.HasSuffix(name, "LICENSE") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	sum := sha256.New()
	urls := []string{"/"}
	for _, name := range names {
		sum.Write([]byte(name))
		sum.Write(files[name])
		urls = append(urls, "/"+name)
	}
	version := hex.EncodeToString(sum.Sum(nil))[:12]
	list, _ := json.Marshal(urls)
	sw := string(files["sw.js"])
	if !strings.Contains(sw, "__FILES__") {
		return errors.New("sw.js has no __FILES__ placeholder")
	}
	sw = strings.Replace(sw, "'__VERSION__'", "'"+version+"'", 1)
	sw = strings.Replace(sw, "__FILES__", string(list), 1)
	files["sw.js"] = []byte(sw)

	for name, data := range files {
		path := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("built %s: %d files, version %s\n", out, len(files), version)
	return nil
}
