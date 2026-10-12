package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var exts = []string{".mp3", ".wav", ".ogg"}

func write(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path string, mod time.Time) { write(t, path, "x"+path, mod) }

func open(t *testing.T, data string) *Library {
	t.Helper()
	l := Open(data, exts)
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func reopen(t *testing.T, l *Library) *Library {
	t.Helper()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return open(t, l.dataDir)
}

func soundByName(t *testing.T, l *Library, name string) Sound {
	t.Helper()
	for _, tab := range l.Tabs() {
		for _, s := range tab.Sounds {
			if s.Name == name {
				return s
			}
		}
	}
	t.Fatalf("no sound named %q", name)
	return Sound{}
}

func TestScanTabsAndInitialIDs(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	touch(t, filepath.Join(root, "old.mp3"), now.Add(-time.Hour))
	touch(t, filepath.Join(root, "new.wav"), now)
	touch(t, filepath.Join(root, "notes.txt"), now)
	touch(t, filepath.Join(root, "memes", "bruh.ogg"), now)
	touch(t, filepath.Join(root, "memes", "deeper", "ignored.mp3"), now)
	_ = os.MkdirAll(filepath.Join(root, "empty"), 0o755)

	lib := open(t, t.TempDir())
	if n := lib.Scan([]string{root}); n != 3 {
		t.Fatalf("scanned %d sounds, want 3", n)
	}
	tabs := lib.Tabs()
	if len(tabs) != 2 || tabs[1].Name != "memes" || tabs[0].ID != ID(root) {
		t.Fatalf("tabs = %+v, want root + memes", tabs)
	}
	if tabs[0].Sounds[0].Name != "new" {
		t.Fatalf("first sound = %q, want newest first", tabs[0].Sounds[0].Name)
	}
	for _, tab := range tabs {
		for _, s := range tab.Sounds {
			// New sounds keep the path-hash IDs of earlier versions.
			if s.ID == 0 || s.ID > maxSafeID || s.ID != ID(s.Path) {
				t.Fatalf("bad id %d for %s", s.ID, s.Path)
			}
		}
	}
	if ID(`C:\Sounds\A.mp3`) != ID(`c:\sounds\a.mp3`) {
		t.Fatal("IDs must ignore path case on Windows")
	}
}

func TestSettingsPersist(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "a.mp3"), time.Now())
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	id := ID(filepath.Join(root, "a.mp3"))

	vol := 150
	if _, ok, err := lib.Update(id, func(s *SoundSettings) { s.Favorite = true; s.LocalVolume = &vol; s.Color = "#ff0000" }); !ok || err != nil {
		t.Fatalf("update: ok=%v err=%v", ok, err)
	}
	if _, err := lib.UpdateLayout(FavoritesKey, func(l *Layout) { l.Mode = "list" }); err != nil {
		t.Fatal(err)
	}

	again := reopen(t, lib)
	again.Scan([]string{root})
	s, _, ok := again.Sound(id)
	if !ok || !s.Favorite || s.LocalVolume == nil || *s.LocalVolume != 150 || s.Color != "#ff0000" {
		t.Fatalf("settings not restored: %+v", s)
	}
	if favs := again.Favorites(); len(favs) != 1 {
		t.Fatalf("favorites = %d, want 1", len(favs))
	}
	if again.Layout(FavoritesKey).Mode != "list" {
		t.Fatal("layout not restored")
	}
}

// A renamed file keeps its ID and settings (matched by file identity).
func TestRenameKeepsIDAndSettings(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "airhorn.mp3")
	touch(t, old, time.Now())
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	id := ID(old)
	lib.Update(id, func(s *SoundSettings) { s.Favorite = true })

	renamed := filepath.Join(root, "AIR HORN (loud).mp3")
	if err := os.Rename(old, renamed); err != nil {
		t.Fatal(err)
	}
	lib.Scan([]string{root})
	s, _, ok := lib.Sound(id)
	if !ok || s.Path != renamed || s.Name != "AIR HORN (loud)" || !s.Favorite {
		t.Fatalf("after rename: ok=%v %+v", ok, s)
	}
	if _, n := lib.Stats(); n != 1 {
		t.Fatalf("sounds = %d, want 1", n)
	}
}

// Without file identity (a copy to another drive), the content hash matches.
func TestMoveMatchedByContent(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	src := filepath.Join(a, "clip.wav")
	write(t, src, "same audio bytes", time.Now())
	write(t, filepath.Join(a, "other.wav"), "same audio bytez", time.Now()) // same size, other content
	lib := open(t, t.TempDir())
	lib.fileID = func(string) string { return "" }
	lib.Scan([]string{a, b})
	id := ID(src)
	lib.Update(id, func(s *SoundSettings) { s.Emoji = "💥" })
	if !lib.SetFingerprint(id, mustFP(t, src), int64(len("same audio bytes")), mtime(t, src)) {
		t.Fatal("SetFingerprint rejected")
	}

	dst := filepath.Join(b, "moved", "clip renamed.wav")
	write(t, dst, "same audio bytes", time.Now())
	os.Remove(src)
	lib.Scan([]string{a, b})
	s, _, ok := lib.Sound(id)
	if !ok || s.Path != dst || s.Emoji != "💥" {
		t.Fatalf("after move: ok=%v %+v", ok, s)
	}
}

func mustFP(t *testing.T, p string) string {
	t.Helper()
	h, err := Fingerprint(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mtime(t *testing.T, p string) time.Time {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

// An edited file keeps its ID and settings but loses its fingerprint, so
// the indexer reads it again.
func TestEditInPlaceClearsFingerprint(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.mp3")
	t0 := time.Now().Add(-time.Hour)
	write(t, p, "v1", t0)
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	id := ID(p)
	lib.SetFingerprint(id, mustFP(t, p), 2, mtime(t, p))
	lib.Update(id, func(s *SoundSettings) { s.Favorite = true })
	rev := lib.Revision()

	write(t, p, "version 2", time.Now())
	lib.Scan([]string{root})
	s, _, _ := lib.Sound(id)
	if s.Fingerprint != "" || !s.Favorite || s.Size != int64(len("version 2")) {
		t.Fatalf("after edit: %+v", s)
	}
	if lib.Revision() == rev {
		t.Fatal("revision did not change")
	}
}

func TestDuplicatesAreSeparateSounds(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.mp3"), "same", time.Now())
	write(t, filepath.Join(root, "copy of a.mp3"), "same", time.Now())
	lib := open(t, t.TempDir())
	if n := lib.Scan([]string{root}); n != 2 {
		t.Fatalf("sounds = %d, want 2", n)
	}
}

// A renamed folder keeps its tab ID, so the phone keeps the tab's layout.
func TestRenamedFolderKeepsTabID(t *testing.T) {
	root := t.TempDir()
	for i := range 3 {
		touch(t, filepath.Join(root, "memes", fmt.Sprintf("s%d.mp3", i)), time.Now())
	}
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	tabID := lib.Tabs()[0].ID
	lib.UpdateLayout(formatID(tabID), func(l *Layout) { l.Mode = "grid-2" })

	if err := os.Rename(filepath.Join(root, "memes"), filepath.Join(root, "Memes 2026")); err != nil {
		t.Fatal(err)
	}
	lib.Scan([]string{root})
	tabs := lib.Tabs()
	if len(tabs) != 1 || tabs[0].ID != tabID || tabs[0].Name != "Memes 2026" {
		t.Fatalf("tabs after folder rename = %+v", tabs)
	}
	if lib.Layout(formatID(tabID)).Mode != "grid-2" {
		t.Fatal("layout lost")
	}
}

// Removing a folder keeps tombstones; re-adding restores settings. Old
// tombstones expire.
func TestTombstonesReviveAndExpire(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.mp3")
	touch(t, p, time.Now())
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	id := ID(p)
	lib.Update(id, func(s *SoundSettings) { s.Favorite = true })

	lib.Scan(nil)
	if _, _, ok := lib.Sound(id); ok {
		t.Fatal("sound of a removed folder is still listed")
	}
	if len(lib.sounds) != 1 {
		t.Fatal("tombstone not kept")
	}
	lib = reopen(t, lib)
	lib.Scan([]string{root})
	if s, _, ok := lib.Sound(id); !ok || !s.Favorite {
		t.Fatalf("not revived: ok=%v %+v", ok, s)
	}

	lib.Scan(nil)
	now := time.Now()
	lib.now = func() time.Time { return now.Add(TombstoneTTL + time.Hour) }
	lib.Scan(nil)
	if len(lib.sounds) != 0 || len(lib.tabs) != 0 {
		t.Fatalf("tombstones not expired: %d sounds, %d tabs", len(lib.sounds), len(lib.tabs))
	}
}

func TestMigratesSoundsJSON(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	here := filepath.Join(root, "here.mp3")
	gone := filepath.Join(root, "elsewhere", "gone.mp3")
	touch(t, here, time.Now())
	vol := 30
	legacy, _ := json.Marshal(map[string]SoundSettings{
		normalize(here): {Favorite: true},
		gone:            {LocalVolume: &vol},
	})
	os.WriteFile(filepath.Join(data, legacyFile), legacy, 0o644)

	lib := open(t, data)
	lib.Scan([]string{root})
	if s, _, _ := lib.Sound(ID(here)); !s.Favorite {
		t.Fatalf("favorite not migrated: %+v", s)
	}
	lib = reopen(t, lib)
	if _, err := os.Stat(filepath.Join(data, legacyFile+".migrated")); err != nil {
		t.Fatalf("sounds.json not retired: %v", err)
	}
	// The missing file's settings come back with its folder.
	touch(t, gone, time.Now())
	lib.Scan([]string{root})
	if s, _, ok := lib.Sound(ID(gone)); !ok || s.LocalVolume == nil || *s.LocalVolume != 30 {
		t.Fatalf("tombstoned legacy settings not restored: ok=%v %+v", ok, s)
	}
}

// Concurrent updates must never corrupt library.json (known issue #1).
func TestConcurrentUpdatesSaveValidJSON(t *testing.T) {
	root := t.TempDir()
	for i := range 20 {
		touch(t, filepath.Join(root, fmt.Sprintf("s%d.mp3", i)), time.Now())
	}
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	var ids []uint64
	for _, s := range lib.Tabs()[0].Sounds {
		ids = append(ids, s.ID)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 50 {
				lib.Update(ids[(i+j)%len(ids)], func(s *SoundSettings) { s.Favorite = !s.Favorite })
				if j%10 == 0 {
					_ = lib.Flush()
				}
			}
		}()
	}
	wg.Wait()
	if err := lib.Flush(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(lib.indexPath())
	var idx indexJSON
	if err := json.Unmarshal(data, &idx); err != nil || len(idx.Sounds) != 20 {
		t.Fatalf("index unreadable or incomplete: %v, %d sounds", err, len(idx.Sounds))
	}
	if lib.IndexSize() != int64(len(data)) {
		t.Fatal("index size not tracked")
	}
}

func TestBrokenIndexIsKeptAside(t *testing.T) {
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, indexFile), []byte("{nope"), 0o644)
	lib := open(t, data)
	matches, _ := filepath.Glob(filepath.Join(data, indexFile+".broken-*"))
	if len(matches) != 1 || len(lib.sounds) != 0 {
		t.Fatalf("broken index not moved aside: %v", matches)
	}
}

func TestFingerprintReadsOnlyHeadAndTail(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 10<<20)
	for i := range big {
		big[i] = byte(i * 7)
	}
	a, b := filepath.Join(dir, "a.mp3"), filepath.Join(dir, "b.mp3")
	os.WriteFile(a, big, 0o644)
	big[len(big)-1] ^= 0xFF // differs in the last byte (e.g. a tag edit)
	os.WriteFile(b, big, 0o644)
	if mustFP(t, a) == mustFP(t, b) {
		t.Fatal("files differing at the end share a fingerprint")
	}
	big[len(big)-1] ^= 0xFF
	big = append(big, 0) // differs only in size
	os.WriteFile(b, big, 0o644)
	if mustFP(t, a) == mustFP(t, b) {
		t.Fatal("files of different sizes share a fingerprint")
	}
}

func TestDurationExactWins(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "a.mp3"), time.Now())
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	s := soundByName(t, lib, "a")
	lib.SetDuration(s.ID, s.Size, s.Modified, 3*time.Second, true)
	lib.SetDuration(s.ID, s.Size, s.Modified, 2*time.Second, false) // a probe must not override
	lib.SetDuration(s.ID, s.Size+1, s.Modified, time.Second, true)  // stale file state
	if got, _, _ := lib.Sound(s.ID); got.Duration != 3*time.Second {
		t.Fatalf("duration = %v", got.Duration)
	}
}

func TestMarkChanged(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.mp3")
	touch(t, p, time.Now())
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	s := soundByName(t, lib, "a")
	lib.SetFingerprint(s.ID, "abc", s.Size, s.Modified)
	if lib.SetFingerprint(s.ID, "def", s.Size+1, s.Modified) {
		t.Fatal("SetFingerprint accepted a stale size")
	}
	lib.MarkChanged(s.ID, s.Size+5, time.Now())
	if s, _, _ := lib.Sound(s.ID); s.Fingerprint != "" {
		t.Fatal("MarkChanged kept the hash")
	}
}
