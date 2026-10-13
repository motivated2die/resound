// Command webapp builds the Resound app: the static web page hosted on HTTPS
// (Netlify) that phones install and that connects to the PC over WebRTC. It
// copies webapp/src and fills in the service worker's version and file list.
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

func main() {
	src := flag.String("src", "webapp/src", "the app's files")
	out := flag.String("out", "webapp/dist", "output folder (replaced)")
	serve := flag.String("serve", "", "serve the result on this address, e.g. :8095 (localhost is a secure context)")
	flag.Parse()

	if err := build(*src, *out); err != nil {
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

func build(src, out string) error {
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
// file, and the list of URLs it caches: everything the app can load.
func precache(files map[string][]byte) (version, list string) {
	var names []string
	for name := range files {
		base := path.Base(name)
		if name == "sw.js" || name == "_headers" || base == "README.md" || strings.HasPrefix(base, "LICENSE") {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	sum := sha256.New()
	urls := []string{"/"}
	for _, name := range names {
		sum.Write([]byte(name))
		sum.Write(files[name])
		urls = append(urls, "/"+name)
	}
	data, _ := json.Marshal(urls)
	return hex.EncodeToString(sum.Sum(nil))[:12], string(data)
}
