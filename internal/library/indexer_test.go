package library

import (
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndexerFingerprintsAndProbes(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.mp3"), "aaaa", time.Now())
	write(t, filepath.Join(root, "b.mp3"), "bbbb", time.Now())
	write(t, filepath.Join(root, "broken.mp3"), "zzzz", time.Now())
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})

	var probes atomic.Int32
	ix := NewIndexer(lib, func(path string) (time.Duration, error) {
		probes.Add(1)
		if strings.Contains(path, "broken") {
			return 0, errors.New("no header")
		}
		return 1500 * time.Millisecond, nil
	})
	ix.pass()
	for _, name := range []string{"a", "b"} {
		s := soundByName(t, lib, name)
		if s.Fingerprint == "" || s.Duration != 1500*time.Millisecond {
			t.Fatalf("%s not indexed: %+v", name, s)
		}
	}
	if b := soundByName(t, lib, "broken"); b.Fingerprint == "" || b.Duration != 0 {
		t.Fatalf("broken: %+v", b)
	}
	if st := ix.Status(); st.Busy || st.Failed != 1 || st.Total != 3 {
		t.Fatalf("status = %+v", st)
	}
	// Nothing left: the failed probe is not retried until the file changes.
	if jobs := ix.pending(); len(jobs) != 0 {
		t.Fatalf("pending after pass: %+v", jobs)
	}

	// An edited file is read again.
	write(t, filepath.Join(root, "a.mp3"), "a, but louder", time.Now().Add(time.Second))
	lib.Scan([]string{root})
	before := probes.Load()
	ix.pass()
	if probes.Load() != before+1 || soundByName(t, lib, "a").Fingerprint == "" {
		t.Fatal("edited file not re-indexed")
	}
}

func TestIndexerRunsInBackground(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.mp3"), "aaaa", time.Now())
	lib := open(t, t.TempDir())
	ix := NewIndexer(lib, func(string) (time.Duration, error) { return time.Second, nil })
	lib.OnChange = ix.Kick
	ix.Start()
	defer ix.Stop()
	lib.Scan([]string{root})
	deadline := time.Now().Add(2 * time.Second)
	for soundByName(t, lib, "a").Duration == 0 {
		if time.Now().After(deadline) {
			t.Fatal("indexer did not run after the scan")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
