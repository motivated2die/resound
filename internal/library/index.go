package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	indexFile   = "library.json"
	legacyFile  = "sounds.json"
	indexFormat = 1
	// saveDelay batches bursts of changes (a scan, a slider drag) into one write.
	saveDelay = 300 * time.Millisecond
)

// legacyEntry is one sounds.json entry from before the index existed.
type legacyEntry struct {
	path     string
	settings SoundSettings
}

// soundRec is one sound's record in the index.
type soundRec struct {
	ID           uint64        `json:"-"`
	Path         string        `json:"path"`
	TabID        uint64        `json:"tab,omitempty"`
	Size         int64         `json:"size"`
	ModTime      time.Time     `json:"mtime"`
	FileID       string        `json:"fileId,omitempty"`
	Fingerprint  string        `json:"fingerprint,omitempty"`
	DurationMs   int64         `json:"durationMs,omitempty"`
	ExactLength  bool          `json:"exactLength,omitempty"` // DurationMs measured by decoding, not probed
	Level        *Level        `json:"level,omitempty"`
	AddedAt      time.Time     `json:"addedAt"`
	MissingSince *time.Time    `json:"missingSince,omitempty"`
	Settings     SoundSettings `json:"settings,omitzero"`
}

func (r *soundRec) missing() bool { return r.MissingSince != nil }

func (r *soundRec) sound() Sound {
	base := filepath.Base(r.Path)
	var level *Level
	if r.Level != nil {
		lv := *r.Level
		level = &lv
	}
	return Sound{
		ID:            r.ID,
		TabID:         r.TabID,
		Name:          strings.TrimSuffix(base, filepath.Ext(base)),
		Path:          r.Path,
		Modified:      r.ModTime,
		Size:          r.Size,
		Fingerprint:   r.Fingerprint,
		Duration:      time.Duration(r.DurationMs) * time.Millisecond,
		Level:         level,
		SoundSettings: r.Settings,
	}
}

// tabRec is one folder (tab) in the index.
type tabRec struct {
	ID           uint64     `json:"-"`
	Path         string     `json:"path"`
	Root         string     `json:"root"`
	MissingSince *time.Time `json:"missingSince,omitempty"`
}

// indexJSON is the file format of library.json. Map keys are decimal IDs.
type indexJSON struct {
	Version int                  `json:"version"`
	Tabs    map[string]*tabRec   `json:"tabs"`
	Sounds  map[string]*soundRec `json:"sounds"`
	Layouts map[string]*Layout   `json:"layouts,omitempty"`
}

func (l *Library) indexPath() string  { return filepath.Join(l.dataDir, indexFile) }
func (l *Library) legacyPath() string { return filepath.Join(l.dataDir, legacyFile) }

// load reads library.json, or prepares the migration of sounds.json.
func (l *Library) load() {
	data, err := os.ReadFile(l.indexPath())
	switch {
	case err == nil:
		var idx indexJSON
		if err := json.Unmarshal(data, &idx); err != nil || idx.Version != indexFormat {
			// Keep the unreadable file for inspection instead of overwriting it.
			bad := l.indexPath() + ".broken-" + time.Now().Format("20060102-150405")
			_ = os.Rename(l.indexPath(), bad)
			l.logf("library: %s is unreadable (%v); moved to %s and starting fresh", indexFile, err, filepath.Base(bad))
			break
		}
		for k, r := range idx.Sounds {
			id, err := strconv.ParseUint(k, 10, 64)
			if err != nil || r == nil || id == 0 || id > maxSafeID {
				continue
			}
			r.ID = id
			l.sounds[id] = r
			l.byPath[normalize(r.Path)] = id
		}
		for k, t := range idx.Tabs {
			id, err := strconv.ParseUint(k, 10, 64)
			if err != nil || t == nil || id == 0 || id > maxSafeID {
				continue
			}
			t.ID = id
			l.tabs[id] = t
		}
		for k, lay := range idx.Layouts {
			if lay != nil && !lay.empty() {
				l.layouts[k] = lay
			}
		}
		l.saver.size = int64(len(data))
		return
	case !errors.Is(err, os.ErrNotExist):
		l.logf("library: reading %s: %v", indexFile, err)
		return
	}

	// First start after the update: per-sound settings were stored by path.
	if data, err := os.ReadFile(l.legacyPath()); err == nil {
		legacy := map[string]SoundSettings{}
		if err := json.Unmarshal(data, &legacy); err == nil {
			l.legacy = map[string]legacyEntry{}
			for p, s := range legacy {
				l.legacy[normalize(p)] = legacyEntry{path: p, settings: s}
			}
		}
	}
}

// migrateLegacyLocked adopts sounds.json settings after the first scan:
// sounds found by path get their settings; the rest become tombstones so
// re-adding their folder later still restores them.
func (l *Library) migrateLegacyLocked() {
	if l.legacy == nil {
		return
	}
	now := l.now()
	adopted, kept := 0, 0
	for norm, e := range l.legacy {
		if e.settings.empty() {
			continue
		}
		if id, ok := l.byPath[norm]; ok {
			l.sounds[id].Settings = e.settings
			adopted++
			continue
		}
		id := l.freeSoundID(ID(norm))
		gone := now
		l.sounds[id] = &soundRec{ID: id, Path: e.path, AddedAt: now, MissingSince: &gone, Settings: e.settings}
		l.byPath[norm] = id
		kept++
	}
	l.legacy = nil
	l.saver.migrated = true
	l.logf("library: migrated %s (%d sound(s) found, %d kept for missing files)", legacyFile, adopted, kept)
}

// freeSoundID returns want, or a free ID derived from it on a collision.
func (l *Library) freeSoundID(want uint64) uint64 {
	id := want
	for i := uint64(1); l.sounds[id] != nil || id == 0; i++ {
		id = (want + i*0x9E3779B97F4A7C15) & maxSafeID
	}
	return id
}

func (l *Library) freeTabID(want uint64) uint64 {
	id := want
	for i := uint64(1); l.tabs[id] != nil || id == 0; i++ {
		id = (want + i*0x9E3779B97F4A7C15) & maxSafeID
	}
	return id
}

// marshalLocked encodes the index. Callers hold at least the read lock.
func (l *Library) marshalLocked() ([]byte, error) {
	idx := indexJSON{
		Version: indexFormat,
		Tabs:    make(map[string]*tabRec, len(l.tabs)),
		Sounds:  make(map[string]*soundRec, len(l.sounds)),
		Layouts: make(map[string]*Layout, len(l.layouts)),
	}
	for id, t := range l.tabs {
		idx.Tabs[strconv.FormatUint(id, 10)] = t
	}
	for id, r := range l.sounds {
		idx.Sounds[strconv.FormatUint(id, 10)] = r
	}
	for k, lay := range l.layouts {
		idx.Layouts[k] = lay
	}
	return json.MarshalIndent(idx, "", " ")
}

// IndexSize returns the size of library.json as last written, in bytes.
func (l *Library) IndexSize() int64 {
	l.saver.mu.Lock()
	defer l.saver.mu.Unlock()
	return l.saver.size
}

// saver writes the index after a short delay. Writes are serialized by
// writeMu and always encode the current state, so a later write can never
// be overtaken by an earlier one.
type saver struct {
	lib *Library

	mu       sync.Mutex
	timer    *time.Timer
	closed   bool
	size     int64
	migrated bool // sounds.json can be retired after the next write
	lastErr  error

	writeMu sync.Mutex
}

func (s *saver) init(l *Library) { s.lib = l }

// schedule queues a write. It returns the last write error, if any.
func (s *saver) schedule() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("library is closed")
	}
	if s.timer == nil {
		s.timer = time.AfterFunc(saveDelay, func() { _ = s.flush() })
	}
	return s.lastErr
}

func (s *saver) flush() error {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	l := s.lib
	l.mu.RLock()
	data, err := l.marshalLocked()
	l.mu.RUnlock()
	if err == nil {
		err = writeAtomic(l.indexPath(), data)
	}
	s.mu.Lock()
	s.lastErr = err
	migrated := s.migrated && err == nil
	if err == nil {
		s.size = int64(len(data))
		s.migrated = false
	}
	s.mu.Unlock()
	if err != nil {
		l.logf("library: saving %s: %v", indexFile, err)
		return err
	}
	if migrated {
		if err := os.Rename(l.legacyPath(), l.legacyPath()+".migrated"); err != nil && !errors.Is(err, os.ErrNotExist) {
			l.logf("library: retiring %s: %v", legacyFile, err)
		}
	}
	return nil
}

func (s *saver) close() error {
	s.mu.Lock()
	pending := s.timer != nil
	s.closed = true
	s.mu.Unlock()
	if pending {
		return s.flush()
	}
	return nil
}

// writeAtomic writes data to a temporary file and renames it over path, so
// a crash never leaves a half-written file behind.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing %s: %w", filepath.Base(path), err)
	}
	return nil
}
