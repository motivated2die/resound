package library

import (
	"errors"
	"os"
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

// The indexer measures levels after the quick stage, keeps them across
// moves and restarts, measures again after an edit or a new measurement
// version, and doesn't retry files it can't decode until they change.
func TestIndexerMeasuresLevels(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.mp3"), "aaaa", time.Now())
	write(t, filepath.Join(root, "long.mp3"), "llll", time.Now())
	write(t, filepath.Join(root, "broken.mp3"), "zzzz", time.Now())
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})

	var measured atomic.Int32
	newIndexer := func(lib *Library, version int) *Indexer {
		ix := NewIndexer(lib, func(string) (time.Duration, error) { return time.Second, nil })
		ix.LevelVersion = version
		ix.Measure = func(path string) (Measurement, error) {
			measured.Add(1)
			switch {
			case strings.Contains(path, "broken"):
				return Measurement{}, errors.New("not audio")
			case strings.Contains(path, "long"):
				return Measurement{LUFS: -20, Peak: -6, Length: 3 * time.Minute}, nil // sampled
			}
			return Measurement{LUFS: -9.5, Peak: -0.5, Length: 1234 * time.Millisecond, Exact: true}, nil
		}
		return ix
	}
	ix := newIndexer(lib, 1)
	ix.pass()
	a := soundByName(t, lib, "a")
	if a.Level == nil || *a.Level != (Level{LUFS: -9.5, Peak: -0.5, Version: 1}) || a.Duration != 1234*time.Millisecond {
		t.Fatalf("a: level %+v, duration %v", a.Level, a.Duration)
	}
	if long := soundByName(t, lib, "long"); long.Level == nil || long.Duration != 3*time.Minute {
		t.Fatalf("long: level %+v, duration %v", long.Level, long.Duration)
	}
	if b := soundByName(t, lib, "broken"); b.Level != nil {
		t.Fatalf("broken has a level: %+v", b.Level)
	}
	if st := ix.Status(); st.Busy || st.Done != 3 || st.Total != 3 {
		t.Fatalf("status = %+v", st)
	}
	if jobs := ix.pending(); len(jobs) != 0 {
		t.Fatalf("pending after pass: %+v", jobs)
	}

	// Moves and restarts keep the level.
	moved := filepath.Join(root, "sub", "a moved.mp3")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "a.mp3"), moved); err != nil {
		t.Fatal(err)
	}
	lib.Scan([]string{root})
	lib = reopen(t, lib)
	lib.Scan([]string{root})
	if a := soundByName(t, lib, "a moved"); a.Level == nil || a.Level.LUFS != -9.5 {
		t.Fatalf("after move and restart: %+v", a.Level)
	}

	// A new measurement version measures everything that can be decoded.
	before := measured.Load()
	ix = newIndexer(lib, 2)
	ix.pass()
	if got := measured.Load() - before; got != 3 { // a fresh indexer tries broken.mp3 once more
		t.Fatalf("measured %d file(s) for the new version, want 3", got)
	}
	if a := soundByName(t, lib, "a moved"); a.Level == nil || a.Level.Version != 2 {
		t.Fatalf("not measured again: %+v", a.Level)
	}

	// An edit clears the level until it is measured again.
	write(t, moved, "a, but louder", time.Now().Add(time.Second))
	lib.Scan([]string{root})
	if a := soundByName(t, lib, "a moved"); a.Level != nil {
		t.Fatalf("edited file kept its level: %+v", a.Level)
	}
	before = measured.Load()
	ix.pass()
	if measured.Load() != before+1 || soundByName(t, lib, "a moved").Level == nil {
		t.Fatal("edited file not measured again")
	}
}

// SetLevel ignores results for a file that changed since it was read.
func TestSetLevelChecksFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.mp3")
	write(t, p, "aaaa", time.Now())
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	s := soundByName(t, lib, "a")
	lib.SetLevel(s.ID, s.Size+1, s.Modified, Level{LUFS: -10})
	if soundByName(t, lib, "a").Level != nil {
		t.Fatal("level stored for another file size")
	}
	lib.SetLevel(s.ID, s.Size, s.Modified, Level{LUFS: -10})
	lib.MarkChanged(s.ID, s.Size+1, s.Modified)
	if soundByName(t, lib, "a").Level != nil {
		t.Fatal("MarkChanged kept the level")
	}
}
