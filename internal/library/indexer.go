package library

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// IndexerStatus is the indexer's progress for display.
type IndexerStatus struct {
	Busy        bool
	Done, Total int
	Failed      int // files whose length could not be read in this session
}

// Indexer reads new and changed sounds in the background: it fingerprints
// them (so moved files are recognized) and probes their length from the file
// headers (for the phone's progress bars). Neither decodes audio: at most a
// few hundred KB are read per file, so even large libraries are indexed in
// seconds. Sounds are playable before they are indexed.
type Indexer struct {
	lib *Library
	// Probe returns a file's length from its headers.
	Probe func(path string) (time.Duration, error)

	// OnProgress is called from the worker goroutine as files complete.
	OnProgress func()
	Logf       func(format string, args ...any)

	kick chan struct{}
	stop chan struct{}
	wg   sync.WaitGroup

	busy        atomic.Bool
	done, total atomic.Int64

	mu     sync.Mutex
	failed map[uint64]time.Time // sound -> modification time that failed to probe
}

// NewIndexer creates an indexer. Call Start to run it.
func NewIndexer(lib *Library, probe func(path string) (time.Duration, error)) *Indexer {
	return &Indexer{lib: lib, Probe: probe, kick: make(chan struct{}, 1), stop: make(chan struct{}), failed: map[uint64]time.Time{}}
}

// Start runs the worker until Stop.
func (ix *Indexer) Start() {
	ix.wg.Add(1)
	go ix.run()
	ix.Kick()
}

// Stop ends the worker after the current file.
func (ix *Indexer) Stop() {
	select {
	case <-ix.stop:
	default:
		close(ix.stop)
	}
	ix.wg.Wait()
}

// Kick asks for a new pass (after a scan or a changed file).
func (ix *Indexer) Kick() {
	select {
	case ix.kick <- struct{}{}:
	default:
	}
}

// Status returns the current progress.
func (ix *Indexer) Status() IndexerStatus {
	ix.mu.Lock()
	failed := len(ix.failed)
	ix.mu.Unlock()
	return IndexerStatus{Busy: ix.busy.Load(), Done: int(ix.done.Load()), Total: int(ix.total.Load()), Failed: failed}
}

func (ix *Indexer) logf(format string, args ...any) {
	if ix.Logf != nil {
		ix.Logf(format, args...)
	}
}

func (ix *Indexer) progress() {
	if ix.OnProgress != nil {
		ix.OnProgress()
	}
}

func (ix *Indexer) run() {
	defer ix.wg.Done()
	for {
		select {
		case <-ix.stop:
			return
		case <-ix.kick:
		}
		ix.pass()
	}
}

// work is one sound that needs its fingerprint or length.
type work struct {
	id   uint64
	path string
	fp   string
	dur  bool // length unknown
	size int64
	mod  time.Time
}

func (ix *Indexer) pending() []work {
	l := ix.lib
	l.mu.RLock()
	defer l.mu.RUnlock()
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var out []work
	for _, tid := range l.order {
		for _, sid := range l.tabSnd[tid] {
			r := l.sounds[sid]
			dur := r.DurationMs == 0
			if failedAt, ok := ix.failed[sid]; ok && failedAt.Equal(r.ModTime) {
				dur = false // unreadable header; the exact length comes from the first play
			}
			if r.Fingerprint != "" && !dur {
				continue
			}
			out = append(out, work{id: sid, path: r.Path, fp: r.Fingerprint, dur: dur, size: r.Size, mod: r.ModTime})
		}
	}
	return out
}

func (ix *Indexer) pass() {
	jobs := ix.pending()
	if len(jobs) == 0 {
		return
	}
	ix.busy.Store(true)
	ix.done.Store(0)
	ix.total.Store(int64(len(jobs)))
	ix.progress()
	start := time.Now()
	for _, w := range jobs {
		select {
		case <-ix.stop:
			ix.busy.Store(false)
			return
		default:
		}
		ix.process(w)
		ix.done.Add(1)
		ix.progress()
	}
	ix.busy.Store(false)
	ix.progress()
	ix.logf("indexer: %d file(s) indexed in %v", len(jobs), time.Since(start).Round(time.Millisecond))
}

func (ix *Indexer) process(w work) {
	if w.fp == "" {
		fp, err := Fingerprint(w.path)
		if err != nil {
			ix.logf("indexer: reading %s: %v", w.path, err)
			return
		}
		// The file must not have changed while it was read.
		if !unchanged(w.path, w.size, w.mod) || !ix.lib.SetFingerprint(w.id, fp, w.size, w.mod) {
			ix.Kick() // a scan or play picks up the new state
			return
		}
	}
	if w.dur && ix.Probe != nil {
		d, err := ix.Probe(w.path)
		if err != nil || d <= 0 {
			ix.mu.Lock()
			ix.failed[w.id] = w.mod
			ix.mu.Unlock()
			ix.logf("indexer: length of %s unknown until it is played: %v", w.path, err)
			return
		}
		ix.lib.SetDuration(w.id, w.size, w.mod, d, false)
	}
}

func unchanged(path string, size int64, mod time.Time) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() == size && info.ModTime().Equal(mod)
}
