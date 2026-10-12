// Command devupdate is a stand-in for GitHub's release API, for testing the
// in-app update locally (docs/BUILDING.md, "Testing updates locally"). It
// serves /releases/latest for one exe, plus the exe and its
// SHA256SUMS.txt, the way a published GitHub release does.
//
//	go run ./cmd/devupdate -exe $HOME\Desktop\Resound-0.9.9\Resound.exe -version 0.9.9
//	$env:RESOUND_UPDATE_API = "http://127.0.0.1:8090"   # then start Resound
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	exe := flag.String("exe", "", "Resound.exe to offer as the latest release (required)")
	version := flag.String("version", "", "its version, e.g. 0.9.9 (required)")
	addr := flag.String("addr", "127.0.0.1:8090", "listen address")
	status := flag.Int("status", 0, "answer /releases/latest with this HTTP status instead (e.g. 404, 403)")
	badSum := flag.Bool("bad-checksum", false, "publish a wrong SHA-256, to test that the update is refused")
	slow := flag.Duration("slow", 0, "spread the exe download over this long, to watch the progress")
	flag.Parse()
	if *exe == "" || *version == "" {
		flag.Usage()
		os.Exit(2)
	}
	data, err := os.ReadFile(*exe)
	if err != nil {
		log.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if *badSum {
		hash = fmt.Sprintf("%064x", 0)
	}
	base := "http://" + *addr
	tag := "v" + *version

	http.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, r.UserAgent())
		if *status != 0 {
			w.WriteHeader(*status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":   tag,
			"name":       "Resound " + *version,
			"html_url":   base + "/notes",
			"draft":      false,
			"prerelease": false,
			"assets": []map[string]any{
				{"name": "Resound.exe", "browser_download_url": base + "/download/Resound.exe", "size": len(data)},
				{"name": "SHA256SUMS.txt", "browser_download_url": base + "/download/SHA256SUMS.txt", "size": 80},
			},
		})
	})
	http.HandleFunc("/download/Resound.exe", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if *slow <= 0 {
			_, _ = w.Write(data)
			return
		}
		const chunks = 50
		for i := range chunks {
			_, _ = w.Write(data[len(data)*i/chunks : len(data)*(i+1)/chunks])
			w.(http.Flusher).Flush()
			time.Sleep(*slow / chunks)
		}
	})
	http.HandleFunc("/download/SHA256SUMS.txt", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		fmt.Fprintf(w, "%s  Resound.exe\n", hash)
	})
	http.HandleFunc("/notes", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Release notes of %s (devupdate)\n", tag)
	})

	log.Printf("offering %s (%s, %d bytes, sha256 %s)", tag, *exe, len(data), hash)
	log.Printf("start Resound with  $env:RESOUND_UPDATE_API = %q", base)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
