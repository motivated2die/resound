// Package library keeps the index of the user's sounds: which files exist,
// their stable IDs, their content hashes and the user's per-sound settings.
//
// Every added folder becomes a tab for its own files, plus one tab per
// subfolder that contains sounds, at any depth (up to MaxDepth levels).
// Nested tabs are named by their path below the added folder ("Anime ›
// Naruto"). Empty folders are no tabs, but Folders lists them, so the panel
// can offer them as places to put sounds.
//
// IDs are assigned once, when a file is first seen, and then never change.
// A file that is renamed, moved to another folder or edited keeps its ID and
// settings: scans match files to known records by path, then by NTFS file
// ID, then by content hash (see reconcile.go). Records of files that went
// missing are kept as tombstones for TombstoneTTL, so settings survive a
// file being moved out and back, or a folder being removed and re-added.
//
// The index lives in <dataDir>/library.json and is written by a debounced,
// serialized saver, so concurrent updates can never interleave writes.
package library

import (
	"hash/fnv"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxSafeID keeps IDs exact in JavaScript numbers (2^53 - 1).
const maxSafeID = 1<<53 - 1

// TombstoneTTL is how long records of missing files and folders are kept.
const TombstoneTTL = 30 * 24 * time.Hour

// FavoritesKey is the layout key of the phone's favorites tab.
const FavoritesKey = "favorites"

// Sound is one playable file.
type Sound struct {
	ID       uint64
	TabID    uint64
	Name     string // file name without extension
	Path     string
	Modified time.Time
	Size     int64
	// Fingerprint identifies the file's content ("" until the indexer read
	// it); scans use it to recognize files moved where file IDs don't help.
	Fingerprint string
	// Duration is known once the indexer probed the file (0 before).
	Duration time.Duration
	SoundSettings
}

// SoundSettings are the user's per-sound choices. They follow the sound
// through renames and moves.
type SoundSettings struct {
	Favorite     bool   `json:"favorite,omitempty"`
	LocalVolume  *int   `json:"localVolume,omitempty"`  // nil = use the default
	RemoteVolume *int   `json:"remoteVolume,omitempty"` // nil = use the default
	Color        string `json:"color,omitempty"`        // phone card color, "" = default
	Emoji        string `json:"emoji,omitempty"`        // phone card emoji, "" = none
}

func (s SoundSettings) empty() bool {
	return !s.Favorite && s.LocalVolume == nil && s.RemoteVolume == nil && s.Color == "" && s.Emoji == ""
}

// Layout is the phone's arrangement of one tab (or of the favorites).
type Layout struct {
	// Mode is the phone's layout mode ("grid-3", "grid-2", "list"; "" = default).
	Mode string `json:"mode,omitempty"`
	// Orders holds a custom sound order (sound IDs) per layout mode.
	Orders map[string][]uint64 `json:"orders,omitempty"`
}

func (l Layout) empty() bool { return l.Mode == "" && len(l.Orders) == 0 }

func (l Layout) clone() Layout {
	out := Layout{Mode: l.Mode}
	if len(l.Orders) > 0 {
		out.Orders = make(map[string][]uint64, len(l.Orders))
		for k, v := range l.Orders {
			out.Orders[k] = slices.Clone(v)
		}
	}
	return out
}

// Tab is one folder's worth of sounds.
type Tab struct {
	ID     uint64
	Name   string
	Path   string
	Root   string // the configured folder this tab belongs to
	Sounds []Sound
}

// Library is safe for concurrent use.
type Library struct {
	extensions []string
	dataDir    string
	now        func() time.Time
	fileID     func(path string) string // NTFS file identity ("" = unknown)

	// OnChange is called (on the scanning goroutine, without locks held)
	// after a scan added, removed, moved or changed sounds.
	OnChange func()
	// Logf receives diagnostics (save errors, migration notes). May be nil.
	Logf func(format string, args ...any)

	scanMu sync.Mutex // serializes scans

	mu       sync.RWMutex
	folders  []string
	sounds   map[uint64]*soundRec // every record, present or tombstoned
	tabs     map[uint64]*tabRec
	layouts  map[string]*Layout
	byPath   map[string]uint64 // normalized path -> sound ID (all records)
	order    []uint64          // present tabs in display order
	tabSnd   map[uint64][]uint64
	tree     map[string][]Folder // normalized library folder -> its subfolders
	scanned  time.Time
	revision uint64
	legacy   map[string]legacyEntry // sounds.json, consumed by the first scan

	saver saver
}

// Open loads the index from <dataDir>/library.json (migrating sounds.json
// from earlier versions). extensions lists the playable file types (lower
// case, with dot). Call Close before exiting so pending changes are saved.
func Open(dataDir string, extensions []string) *Library {
	l := &Library{
		extensions: extensions,
		dataDir:    dataDir,
		now:        time.Now,
		fileID:     fileID,
		sounds:     map[uint64]*soundRec{},
		tabs:       map[uint64]*tabRec{},
		layouts:    map[string]*Layout{},
		byPath:     map[string]uint64{},
		tabSnd:     map[uint64][]uint64{},
	}
	l.saver.init(l)
	l.load()
	return l
}

// Close writes pending changes and stops the background saver.
func (l *Library) Close() error { return l.saver.close() }

// Flush writes pending changes now.
func (l *Library) Flush() error { return l.saver.flush() }

// ID derives the initial ID of a sound or tab from its path. A record keeps
// the ID it got when it was first seen, even after it moves, so only new
// records use this. Using the path hash for them keeps IDs from earlier
// versions (which were always path hashes) valid on existing phones.
func ID(path string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(normalize(path)))
	id := h.Sum64() & maxSafeID
	if id == 0 {
		id = 1
	}
	return id
}

func formatID(id uint64) string { return strconv.FormatUint(id, 10) }

func normalize(path string) string { return strings.ToLower(filepath.Clean(path)) }

func (l *Library) logf(format string, args ...any) {
	if l.Logf != nil {
		l.Logf(format, args...)
	}
}

// Rescan repeats the last scan if it is older than maxAge.
func (l *Library) Rescan(maxAge time.Duration) {
	l.mu.RLock()
	stale := time.Since(l.scanned) > maxAge
	folders := l.folders
	l.mu.RUnlock()
	if stale {
		l.Scan(folders)
	}
}

// Revision increases whenever anything the phone shows changes: sounds,
// tabs, settings or layouts. It is not persisted.
func (l *Library) Revision() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.revision
}

// Tabs returns all present tabs with their present sounds.
func (l *Library) Tabs() []Tab {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Tab, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, l.tabLocked(id))
	}
	return out
}

// Tab returns one present tab.
func (l *Library) Tab(id uint64) (Tab, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if !slices.Contains(l.order, id) {
		return Tab{}, false
	}
	return l.tabLocked(id), true
}

func (l *Library) tabLocked(id uint64) Tab {
	t := l.tabs[id]
	tab := Tab{ID: id, Name: tabName(t), Path: t.Path, Root: t.Root}
	for _, sid := range l.tabSnd[id] {
		tab.Sounds = append(tab.Sounds, l.sounds[sid].sound())
	}
	return tab
}

// tabName is a folder's name, or for nested folders their path below the
// subfolder of the library folder: "Anime › Naruto". Direct subfolders keep
// their plain name, as before nesting was scanned.
func tabName(t *tabRec) string {
	if t.Root != "" {
		if rel, err := filepath.Rel(t.Root, t.Path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return strings.Join(strings.Split(rel, string(filepath.Separator)), " › ")
		}
	}
	return filepath.Base(t.Path)
}

// Folder is a folder inside a library folder, found by the last scan, with
// or without sounds. Depth is 1 for a direct subfolder.
type Folder struct {
	Path  string
	Depth int
}

// Folders returns the subfolders of library folder root found by the last
// scan, in display order (each followed by its own subfolders).
func (l *Library) Folders(root string) []Folder {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return slices.Clone(l.tree[normalize(root)])
}

// Sound returns one present sound and the name of its tab.
func (l *Library) Sound(id uint64) (Sound, string, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	r, ok := l.sounds[id]
	if !ok || r.missing() {
		return Sound{}, "", false
	}
	name := ""
	if t := l.tabs[r.TabID]; t != nil {
		name = tabName(t)
	}
	return r.sound(), name, true
}

// Favorites returns all present favorite sounds in tab order.
func (l *Library) Favorites() []Sound {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var out []Sound
	for _, tid := range l.order {
		for _, sid := range l.tabSnd[tid] {
			if r := l.sounds[sid]; r.Settings.Favorite {
				out = append(out, r.sound())
			}
		}
	}
	return out
}

// Stats returns the present tab and sound counts.
func (l *Library) Stats() (tabs, sounds int) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, id := range l.order {
		sounds += len(l.tabSnd[id])
	}
	return len(l.order), sounds
}

// Update changes a present sound's settings and persists them. fn may modify
// the settings in place. It reports whether the sound exists.
func (l *Library) Update(id uint64, fn func(*SoundSettings)) (Sound, bool, error) {
	l.mu.Lock()
	r, ok := l.sounds[id]
	if !ok || r.missing() {
		l.mu.Unlock()
		return Sound{}, false, nil
	}
	fn(&r.Settings)
	l.revision++
	s := r.sound()
	l.mu.Unlock()
	return s, true, l.saver.schedule()
}

// FindPath returns the ID of the present sound at path.
func (l *Library) FindPath(path string) (uint64, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	id, ok := l.byPath[normalize(path)]
	if !ok || l.sounds[id].missing() {
		return 0, false
	}
	return id, true
}

// ResetAppearance clears every sound's color and emoji and every layout.
// Favorites and volumes are kept.
func (l *Library) ResetAppearance() error {
	l.mu.Lock()
	for _, r := range l.sounds {
		r.Settings.Color, r.Settings.Emoji = "", ""
	}
	clear(l.layouts)
	l.revision++
	l.mu.Unlock()
	return l.saver.schedule()
}

// MapVolumes replaces every custom per-sound volume v with fn(v), including
// those of missing sounds, and returns how many sounds it changed.
func (l *Library) MapVolumes(fn func(int) int) (int, error) {
	l.mu.Lock()
	n := 0
	for _, r := range l.sounds {
		changed := false
		for _, p := range []*int{r.Settings.LocalVolume, r.Settings.RemoteVolume} {
			if p != nil {
				if v := fn(*p); v != *p {
					*p, changed = v, true
				}
			}
		}
		if changed {
			n++
		}
	}
	if n > 0 {
		l.revision++
	}
	l.mu.Unlock()
	if n == 0 {
		return 0, nil
	}
	return n, l.saver.schedule()
}

// Layout returns the phone layout stored under key (a tab ID or FavoritesKey).
func (l *Library) Layout(key string) Layout {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if lay := l.layouts[key]; lay != nil {
		return lay.clone()
	}
	return Layout{}
}

// Layouts returns all stored layouts by key.
func (l *Library) Layouts() map[string]Layout {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]Layout, len(l.layouts))
	for k, v := range l.layouts {
		out[k] = v.clone()
	}
	return out
}

// UpdateLayout changes the layout stored under key and persists it.
func (l *Library) UpdateLayout(key string, fn func(*Layout)) (Layout, error) {
	l.mu.Lock()
	lay := Layout{}
	if cur := l.layouts[key]; cur != nil {
		lay = cur.clone()
	}
	fn(&lay)
	if lay.empty() {
		delete(l.layouts, key)
	} else {
		stored := lay.clone()
		l.layouts[key] = &stored
	}
	l.revision++
	l.mu.Unlock()
	return lay, l.saver.schedule()
}

// MarkChanged records that a sound's file changed on disk (new size or
// modification time) without waiting for the next scan. Its fingerprint and
// duration are cleared, so the indexer reads it again.
func (l *Library) MarkChanged(id uint64, size int64, mod time.Time) {
	l.mu.Lock()
	r, ok := l.sounds[id]
	if !ok || (r.Size == size && r.ModTime.Equal(mod)) {
		l.mu.Unlock()
		return
	}
	r.Size, r.ModTime = size, mod
	r.Fingerprint, r.DurationMs, r.ExactLength = "", 0, false
	l.revision++
	l.mu.Unlock()
	_ = l.saver.schedule()
}

// SetFingerprint stores a fingerprint computed by the indexer. It is ignored
// (and returns false) when the file changed since: size and modification
// time must still match the record.
func (l *Library) SetFingerprint(id uint64, fp string, size int64, mod time.Time) bool {
	l.mu.Lock()
	r, ok := l.sounds[id]
	if !ok || r.Size != size || !r.ModTime.Equal(mod) {
		l.mu.Unlock()
		return false
	}
	changed := r.Fingerprint != fp
	r.Fingerprint = fp
	l.mu.Unlock()
	if changed {
		_ = l.saver.schedule()
	}
	return true
}

// SetDuration stores a sound's length, if its file is still the one with
// this size and modification time. exact marks a length measured by
// decoding the whole file; a probed length never replaces an exact one.
func (l *Library) SetDuration(id uint64, size int64, mod time.Time, d time.Duration, exact bool) {
	ms := d.Milliseconds()
	l.mu.Lock()
	r, ok := l.sounds[id]
	if !ok || r.Size != size || !r.ModTime.Equal(mod) || ms <= 0 || (r.ExactLength && !exact) ||
		(r.DurationMs == ms && r.ExactLength == exact) {
		l.mu.Unlock()
		return
	}
	r.DurationMs, r.ExactLength = ms, exact
	l.revision++
	l.mu.Unlock()
	_ = l.saver.schedule()
}
