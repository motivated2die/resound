package library

import (
	"os"
	"runtime"
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

// Measurement is what Indexer.Measure finds out about a sound file.
type Measurement struct {
	LUFS, Peak float64
	// Length is the file's length (0 = unknown), exact when Exact: the
	// whole file was decoded.
	Length time.Duration
	Exact  bool
}

// Indexer reads new and changed sounds in the background, in two stages.
// First it fingerprints them (so moved files are recognized) and probes
// their length from the file headers (for the phone's progress bars). This
// reads at most a few hundred KB per file without decoding audio, so even
// large libraries take seconds. Then, if Measure is set, it measures their
// loudness, which decodes audio and so runs on several workers. Sounds are
// playable before they are indexed.
type Indexer struct {
	lib *Library
	// Probe returns a file's length from its headers.
	Probe func(path string) (time.Duration, error)
	// Measure measures a file's loudness (nil = don't). Levels of another
	// LevelVersion are measured again.
	Measure      func(path string) (Measurement, error)
	LevelVersion int
	// Workers is the number of files measured at once.
	Workers int

	// OnProgress is called from the worker goroutines as files complete.
	OnProgress func()
	Logf       func(format string, args ...any)

	kick chan struct{}
	stop chan struct{}
	wg   sync.WaitGroup

	busy        atomic.Bool
	done, total atomic.Int64

	mu          sync.Mutex
	failed      map[uint64]time.Time // sound -> modification time that failed to probe
	failedLevel map[uint64]time.Time // sound -> modification time that failed to measure
}

// NewIndexer creates an indexer. Call Start to run it.
func NewIndexer(lib *Library, probe func(path string) (time.Duration, error)) *Indexer {
	return &Indexer{
		lib: lib, Probe: probe,
		Workers: min(4, max(1, runtime.NumCPU()/2)),
		kick:    make(chan struct{}, 1), stop: make(chan struct{}),
		failed: map[uint64]time.Time{}, failedLevel: map[uint64]time.Time{},
	}
}

// Start runs the worker until Stop.
func (ix *Indexer) Start() {
	ix.wg.Add(1)
	go ix.run()
	ix.Kick()
}

// Stop ends the worker after the current files.
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

func (ix *Indexer) stopped() bool {
	select {
	case <-ix.stop:
		return true
	default:
		return false
	}
}

// work is one sound that needs its fingerprint, length or level.
type work struct {
	id    uint64
	path  string
	fp    string
	dur   bool // length unknown
	level bool // level unknown or of another version
	size  int64
	mod   time.Time
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
			level := ix.Measure != nil && (r.Level == nil || r.Level.Version != ix.LevelVersion)
			if failedAt, ok := ix.failedLevel[sid]; ok && failedAt.Equal(r.ModTime) {
				level = false // undecodable; tried again when the file changes
			}
			if r.Fingerprint != "" && !dur && !level {
				continue
			}
			out = append(out, work{id: sid, path: r.Path, fp: r.Fingerprint, dur: dur, level: level, size: r.Size, mod: r.ModTime})
		}
	}
	return out
}

// pass reads every pending sound: first the quick stage for all of them,
// then the levels. Done counts sounds that have nothing left to read.
func (ix *Indexer) pass() {
	jobs := ix.pending()
	if len(jobs) == 0 {
		return
	}
	ix.busy.Store(true)
	defer func() {
		ix.busy.Store(false)
		ix.progress()
	}()
	ix.done.Store(0)
	ix.total.Store(int64(len(jobs)))
	ix.progress()
	start := time.Now()
	var levels []work
	for _, w := range jobs {
		if ix.stopped() {
			return
		}
		if (w.fp == "" || w.dur) && !ix.process(w) {
			w.level = false // unreadable or changed meanwhile: the next pass decides
		}
		if w.level {
			levels = append(levels, w)
			continue
		}
		ix.done.Add(1)
		ix.progress()
	}
	if len(levels) > 0 {
		ix.measureAll(levels)
	}
	if ix.stopped() {
		return
	}
	ix.logf("indexer: %d file(s) indexed in %v (%d measured)", len(jobs), time.Since(start).Round(time.Millisecond), len(levels))
}

// process fingerprints and probes one sound. It reports false when the
// file couldn't be read or changed while it was.
func (ix *Indexer) process(w work) bool {
	if w.fp == "" {
		fp, err := Fingerprint(w.path)
		if err != nil {
			ix.logf("indexer: reading %s: %v", w.path, err)
			return false
		}
		// The file must not have changed while it was read.
		if !unchanged(w.path, w.size, w.mod) || !ix.lib.SetFingerprint(w.id, fp, w.size, w.mod) {
			ix.Kick() // a scan or play picks up the new state
			return false
		}
	}
	if w.dur && ix.Probe != nil {
		d, err := ix.Probe(w.path)
		if err != nil || d <= 0 {
			ix.mu.Lock()
			ix.failed[w.id] = w.mod
			ix.mu.Unlock()
			ix.logf("indexer: length of %s unknown until it is played: %v", w.path, err)
			return true // the level may still be measured
		}
		ix.lib.SetDuration(w.id, w.size, w.mod, d, false)
	}
	return true
}

// measureAll measures the levels of jobs on ix.Workers goroutines.
func (ix *Indexer) measureAll(jobs []work) {
	next := make(chan work)
	var wg sync.WaitGroup
	for range max(1, ix.Workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range next {
				ix.measure(w)
				ix.done.Add(1)
				ix.progress()
			}
		}()
	}
	defer wg.Wait()
	defer close(next)
	for _, w := range jobs {
		select {
		case <-ix.stop:
			return
		case next <- w:
		}
	}
}

func (ix *Indexer) measure(w work) {
	m, err := ix.Measure(w.path)
	if err != nil {
		ix.mu.Lock()
		ix.failedLevel[w.id] = w.mod
		ix.mu.Unlock()
		ix.logf("indexer: loudness of %s unknown, it plays unchanged: %v", w.path, err)
		return
	}
	if !unchanged(w.path, w.size, w.mod) {
		ix.Kick()
		return
	}
	ix.lib.SetLevel(w.id, w.size, w.mod, Level{LUFS: m.LUFS, Peak: m.Peak, Version: ix.LevelVersion})
	if m.Length > 0 {
		ix.lib.SetDuration(w.id, w.size, w.mod, m.Length, m.Exact)
	}
}

func unchanged(path string, size int64, mod time.Time) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() == size && info.ModTime().Equal(mod)
}
