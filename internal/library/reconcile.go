package library

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// foundFile is a playable file seen by a scan.
type foundFile struct {
	path, norm string
	dir        string // normalized folder (tab) path
	size       int64
	mod        time.Time
}

// foundDir is a folder with at least one playable file: a tab.
type foundDir struct {
	path, norm, root string
}

// walk lists the tabs and files under folders: each folder plus its direct
// subfolders. It does I/O only and touches no library state.
func (l *Library) walk(folders []string) ([]foundDir, []foundFile) {
	var dirs []foundDir
	var files []foundFile
	seen := map[string]bool{}
	add := func(dir, root string) {
		norm := normalize(dir)
		if seen[norm] {
			return
		}
		seen[norm] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		n := 0
		for _, e := range entries {
			if e.IsDir() || !slices.Contains(l.extensions, strings.ToLower(filepath.Ext(e.Name()))) {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			p := filepath.Join(dir, e.Name())
			files = append(files, foundFile{path: p, norm: normalize(p), dir: norm, size: info.Size(), mod: info.ModTime()})
			n++
		}
		if n > 0 {
			dirs = append(dirs, foundDir{path: dir, norm: norm, root: root})
		}
	}
	for _, root := range folders {
		root = filepath.Clean(root)
		add(root, root)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				add(filepath.Join(root, e.Name()), root)
			}
		}
	}
	return dirs, files
}

// known is what a scan needs to know about an existing record.
type known struct {
	id      uint64
	norm    string
	size    int64
	mod     time.Time
	fileID  string
	fp      string
	tabID   uint64
	missing *time.Time
}

// match is the outcome for one found file.
type match struct {
	f       foundFile
	id      uint64 // 0 = new record
	moved   bool   // matched by file ID or content, not by path
	changed bool   // content may differ from the record (size or time)
	fileID  string // "" = unchanged / unknown
	fp      string // computed during the scan, "" = not computed
}

// Scan rebuilds the tabs from folders and reconciles the files it finds with
// the index. It returns the number of present sounds.
func (l *Library) Scan(folders []string) int {
	l.scanMu.Lock()
	defer l.scanMu.Unlock()

	dirs, files := l.walk(folders)

	// Snapshot the records; matching does I/O and must not hold the lock.
	l.mu.RLock()
	byNorm := make(map[string]known, len(l.sounds))
	for _, r := range l.sounds {
		byNorm[normalize(r.Path)] = known{id: r.ID, norm: normalize(r.Path), size: r.Size, mod: r.ModTime,
			fileID: r.FileID, fp: r.Fingerprint, tabID: r.TabID, missing: r.MissingSince}
	}
	l.mu.RUnlock()

	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[f.norm] = true
	}
	// Orphans: records whose path was not found. Moved files are matched
	// against them; each can be claimed once.
	var orphans []known
	for norm, k := range byNorm {
		if !present[norm] {
			orphans = append(orphans, k)
		}
	}
	claimed := map[uint64]bool{}
	orphanByFileID := map[string][]known{}
	orphanBySize := map[int64][]known{}
	for _, o := range orphans {
		if o.fileID != "" {
			orphanByFileID[o.fileID] = append(orphanByFileID[o.fileID], o)
		}
		if o.fp != "" {
			orphanBySize[o.size] = append(orphanBySize[o.size], o)
		}
	}

	matches := make([]match, len(files))
	for i, f := range files {
		m := match{f: f}
		if k, ok := byNorm[f.norm]; ok {
			m.id = k.id
			m.changed = k.size != f.size || !k.mod.Equal(f.mod)
			if k.fileID == "" || m.changed {
				m.fileID = l.fileID(f.path)
			}
			matches[i] = m
			continue
		}
		// Not at a known path: a renamed or moved file, or a new one.
		m.fileID = l.fileID(f.path)
		if m.fileID != "" {
			if o, ok := pick(orphanByFileID[m.fileID], f, claimed); ok {
				m.id, m.moved = o.id, true
				m.changed = o.size != f.size || !o.mod.Equal(f.mod)
				claimed[o.id] = true
				matches[i] = m
				continue
			}
		}
		// A content match is only possible with an orphan of the same size,
		// so most new files are never read during a scan.
		if cands := orphanBySize[f.size]; len(cands) > 0 {
			if h, err := Fingerprint(f.path); err == nil {
				m.fp = h
				var same []known
				for _, o := range cands {
					if o.fp == h {
						same = append(same, o)
					}
				}
				if o, ok := pick(same, f, claimed); ok {
					m.id, m.moved = o.id, true
					m.changed = !o.mod.Equal(f.mod)
					claimed[o.id] = true
				}
			}
		}
		matches[i] = m
	}

	changed := l.apply(folders, dirs, matches)
	if changed && l.OnChange != nil {
		l.OnChange()
	}
	_, n := l.Stats()
	return n
}

// pick chooses among orphan candidates for f: an unclaimed one with the same
// file name first, then one in the same folder, then the most recently seen.
func pick(cands []known, f foundFile, claimed map[uint64]bool) (known, bool) {
	best, bestScore := known{}, -1
	name := filepath.Base(f.norm)
	for _, c := range cands {
		if claimed[c.id] {
			continue
		}
		score := 0
		if filepath.Base(c.norm) == name {
			score += 2
		}
		if filepath.Dir(c.norm) == f.dir {
			score++
		}
		if score > bestScore || (score == bestScore && laterMissing(c, best)) {
			best, bestScore = c, score
		}
	}
	return best, bestScore >= 0
}

func laterMissing(a, b known) bool {
	if a.missing == nil || b.missing == nil {
		return a.missing == nil && b.missing != nil
	}
	return a.missing.After(*b.missing)
}

// apply writes the scan's matches into the index. It reports whether
// anything the phone shows changed.
func (l *Library) apply(folders []string, dirs []foundDir, matches []match) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	changed := false              // something the phone shows changed
	dirty := false                // only bookkeeping changed (file IDs, fingerprints)
	oldTab := map[uint64]uint64{} // sound -> tab before this scan (moved sounds)
	presentIDs := make(map[uint64]bool, len(matches))
	for _, m := range matches {
		f := m.f
		r := l.sounds[m.id]
		if m.id == 0 || r == nil {
			id := l.freeSoundID(ID(f.path))
			r = &soundRec{ID: id, Path: f.path, Size: f.size, ModTime: f.mod, FileID: m.fileID, Fingerprint: m.fp, AddedAt: now}
			l.sounds[id] = r
			changed = true
		} else {
			if m.moved {
				oldTab[r.ID] = r.TabID
				delete(l.byPath, normalize(r.Path))
			}
			if r.Path != f.path {
				r.Path = f.path // moved, renamed, or only the letter case changed
				changed = true
			}
			if m.changed {
				r.Size, r.ModTime = f.size, f.mod
				r.Fingerprint, r.DurationMs, r.ExactLength = m.fp, 0, false
				changed = true
			} else if m.fp != "" && r.Fingerprint == "" {
				r.Fingerprint = m.fp
				dirty = true
			}
			if m.fileID != "" && m.fileID != r.FileID {
				r.FileID = m.fileID
				dirty = true
			}
			if r.MissingSince != nil {
				r.MissingSince = nil
				changed = true
			}
		}
		l.byPath[f.norm] = r.ID
		presentIDs[r.ID] = true
	}

	// Records not found: tombstone them, and forget old tombstones.
	for id, r := range l.sounds {
		if presentIDs[id] {
			continue
		}
		if r.MissingSince == nil {
			gone := now
			r.MissingSince = &gone
			changed = true
		} else if now.Sub(*r.MissingSince) > TombstoneTTL {
			delete(l.byPath, normalize(r.Path))
			delete(l.sounds, id)
			dirty = true
		}
	}

	// Tabs: by path, or inherited from a vanished folder whose sounds moved
	// here (a renamed folder), or new.
	tabByNorm := map[string]uint64{}
	for id, t := range l.tabs {
		tabByNorm[normalize(t.Path)] = id
	}
	dirSounds := map[string][]uint64{}
	for _, m := range matches {
		dirSounds[m.f.dir] = append(dirSounds[m.f.dir], l.byPath[m.f.norm])
	}
	foundDirs := map[string]bool{}
	for _, d := range dirs {
		foundDirs[d.norm] = true
	}
	var order []uint64
	tabSnd := map[uint64][]uint64{}
	tabClaimed := map[uint64]bool{}
	for _, d := range dirs {
		id, ok := tabByNorm[d.norm]
		if !ok {
			id, ok = l.inheritTab(dirSounds[d.norm], oldTab, foundDirs, tabClaimed)
		}
		t := l.tabs[id]
		if !ok || t == nil {
			id = l.freeTabID(ID(d.path))
			t = &tabRec{ID: id}
			l.tabs[id] = t
			changed = true
		}
		tabClaimed[id] = true
		if t.Path != d.path || t.Root != d.root || t.MissingSince != nil {
			t.Path, t.Root, t.MissingSince = d.path, d.root, nil
			changed = true
		}
		order = append(order, id)
		for _, sid := range dirSounds[d.norm] {
			r := l.sounds[sid]
			if r.TabID != id {
				r.TabID = id
				changed = true
			}
		}
		ids := slices.Clone(dirSounds[d.norm])
		// Soundux's default order: newest first.
		slices.SortStableFunc(ids, func(a, b uint64) int { return l.sounds[b].ModTime.Compare(l.sounds[a].ModTime) })
		tabSnd[id] = ids
	}
	for id, t := range l.tabs {
		if tabClaimed[id] {
			continue
		}
		if t.MissingSince == nil {
			gone := now
			t.MissingSince = &gone
			changed = true
		} else if now.Sub(*t.MissingSince) > TombstoneTTL {
			delete(l.tabs, id)
			delete(l.layouts, tabKey(id))
			dirty = true
		}
	}
	if !slices.Equal(order, l.order) {
		changed = true
	}
	for id, ids := range tabSnd {
		if !slices.Equal(ids, l.tabSnd[id]) {
			changed = true
		}
	}

	l.order, l.tabSnd = order, tabSnd
	l.folders = slices.Clone(folders)
	l.scanned = time.Now()
	if l.legacy != nil {
		l.migrateLegacyLocked()
		changed = true
	}
	if changed {
		l.revision++
	}
	if changed || dirty {
		_ = l.saver.schedule()
	}
	return changed
}

// inheritTab finds the vanished tab most of a new folder's sounds came from.
// At least half of the folder's sounds must have moved from that one tab.
func (l *Library) inheritTab(sounds []uint64, oldTab map[uint64]uint64, foundDirs map[string]bool, claimed map[uint64]bool) (uint64, bool) {
	counts := map[uint64]int{}
	for _, sid := range sounds {
		if tid, ok := oldTab[sid]; ok && tid != 0 {
			counts[tid]++
		}
	}
	best, bestN := uint64(0), 0
	for tid, n := range counts {
		t := l.tabs[tid]
		if t == nil || claimed[tid] || foundDirs[normalize(t.Path)] {
			continue // that folder still exists, or another folder took it
		}
		if n > bestN || (n == bestN && tid < best) {
			best, bestN = tid, n
		}
	}
	return best, bestN > 0 && bestN*2 >= len(sounds)
}

func tabKey(id uint64) string { return formatID(id) }

// fingerprintSpan is how much of the start and of the end of a file the
// fingerprint covers.
const fingerprintSpan = 64 << 10

// Fingerprint identifies a file's content cheaply: the first 128 bits of
// SHA-256 over its size, its first 64 KB and its last 64 KB. Audio files
// differ in their headers, frame data and tags well within that span, and
// at most 128 KB are read however large the file is.
func Fingerprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	h := sha256.New()
	var sz [8]byte
	binary.LittleEndian.PutUint64(sz[:], uint64(size))
	h.Write(sz[:])
	if size <= 2*fingerprintSpan {
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
	} else {
		buf := make([]byte, fingerprintSpan)
		if _, err := io.ReadFull(f, buf); err != nil {
			return "", err
		}
		h.Write(buf)
		if _, err := f.ReadAt(buf, size-fingerprintSpan); err != nil {
			return "", err
		}
		h.Write(buf)
	}
	return hex.EncodeToString(h.Sum(nil)[:16]), nil
}
