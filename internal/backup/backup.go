// Package backup saves the app's settings into a zip file and restores them.
//
// A backup holds the files in Files (devices, volumes, folders, push-to-talk,
// the sound index with favorites, volumes, colors and phone layouts, and
// the phone PIN and pairings) plus manifest.json, which identifies it. Sounds
// themselves are not included; they stay in the user's folders. A separate
// zip of the sound folders can be made with ZipSounds (sounds.go).
//
// Backups made by the app are listed in <dataDir>/backups/index.json, also
// when saved elsewhere. Zips found in the backups folder are listed too, so
// copying one there is enough.
package backup

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Files are the data files a backup contains, relative to the data folder.
var Files = []string{"config.json", "library.json", "remote.json"}

const (
	appName      = "Resound"
	manifestName = "manifest.json"
	indexName    = "index.json"
	maxFileSize  = 64 << 20 // per file inside a backup; guards against zip bombs
	formatV1     = 1
)

// Manifest describes a backup; it is stored in the zip as manifest.json.
type Manifest struct {
	App     string    `json:"app"`
	Format  int       `json:"format"`
	Version string    `json:"version"` // app version that made it
	Created time.Time `json:"created"`
	Host    string    `json:"host,omitempty"`
	Files   []string  `json:"files"`
}

// Entry is one known backup.
type Entry struct {
	Path    string    `json:"path"`
	Created time.Time `json:"created"`
	Version string    `json:"version"`
	Size    int64     `json:"size"`
}

// Dir is the default backup folder, next to the settings.
func Dir(dataDir string) string { return filepath.Join(dataDir, "backups") }

// DefaultName names a backup after its time, e.g.
// "Resound-backup-2026-10-12_13-05-09.zip". suffix, if set, is appended
// ("before-restore").
func DefaultName(t time.Time, suffix string) string {
	name := "Resound-backup-" + t.Format("2006-01-02_15-04-05")
	if suffix != "" {
		name += "-" + suffix
	}
	return name + ".zip"
}

// Create writes a backup of dataDir to dest (a .zip path) and records it in
// the index. Missing data files are left out (e.g. before the first pairing).
func Create(dataDir, dest, version string, now time.Time) (Entry, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Entry{}, err
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return Entry{}, err
	}
	err = writeZip(f, dataDir, version, now)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		os.Remove(tmp)
		return Entry{}, err
	}
	info, err := os.Stat(dest)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Path: dest, Created: now, Version: version, Size: info.Size()}
	return e, addToIndex(dataDir, e)
}

func writeZip(w io.Writer, dataDir, version string, now time.Time) error {
	zw := zip.NewWriter(w)
	m := Manifest{App: appName, Format: formatV1, Version: version, Created: now}
	m.Host, _ = os.Hostname()
	for _, name := range Files {
		data, err := os.ReadFile(filepath.Join(dataDir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
		m.Files = append(m.Files, name)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: manifestName, Method: zip.Deflate, Modified: now})
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	return zw.Close()
}

// ErrNotBackup means the file is not a Resound backup.
var ErrNotBackup = errors.New("not a Resound backup")

// Read checks a backup and returns its manifest and files. Only the known
// data files are accepted, each must be valid JSON, and none may be larger
// than maxFileSize.
func Read(path string) (Manifest, map[string][]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: %v", ErrNotBackup, err)
	}
	defer zr.Close()
	files := map[string][]byte{}
	for _, f := range zr.File {
		if f.Name != manifestName && !slices.Contains(Files, f.Name) {
			return Manifest{}, nil, fmt.Errorf("%w: unexpected file %q", ErrNotBackup, f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return Manifest{}, nil, fmt.Errorf("%w: %v", ErrNotBackup, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxFileSize+1))
		rc.Close()
		if err != nil {
			return Manifest{}, nil, fmt.Errorf("%w: %v", ErrNotBackup, err)
		}
		if len(data) > maxFileSize || !json.Valid(data) {
			return Manifest{}, nil, fmt.Errorf("%w: %s is damaged", ErrNotBackup, f.Name)
		}
		files[f.Name] = data
	}
	var m Manifest
	if err := json.Unmarshal(files[manifestName], &m); err != nil || m.App != appName {
		return Manifest{}, nil, fmt.Errorf("%w: no manifest", ErrNotBackup)
	}
	if m.Format > formatV1 {
		return Manifest{}, nil, fmt.Errorf("this backup was made by a newer Resound (v%s)", m.Version)
	}
	delete(files, manifestName)
	return m, files, nil
}

// Restore replaces the data files in dataDir with the backup's. It is a
// clean restore: data files the backup doesn't contain are removed, so the
// result matches the state the backup was made in. The app must not be
// running (it would overwrite the files when it saves on exit).
func Restore(dataDir, path string) (Manifest, error) {
	m, files, err := Read(path)
	if err != nil {
		return m, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return m, err
	}
	// Write everything first, then swap it in, so a failed write changes nothing.
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dataDir, name+".restore"), data, 0o644); err != nil {
			removeTemps(dataDir)
			return m, err
		}
	}
	for _, name := range Files {
		target := filepath.Join(dataDir, name)
		if _, ok := files[name]; ok {
			err = os.Rename(target+".restore", target)
		} else if err = os.Remove(target); errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		if err != nil {
			removeTemps(dataDir)
			return m, err
		}
	}
	return m, nil
}

func removeTemps(dataDir string) {
	for _, name := range Files {
		os.Remove(filepath.Join(dataDir, name+".restore"))
	}
}

// List returns the known backups that still exist, newest first: those in
// the index plus zips in the backup folder. Entries whose file is gone are
// dropped from the index.
func List(dataDir string) []Entry {
	idx := readIndex(dataDir)
	var out []Entry
	seen := map[string]bool{}
	for _, e := range idx {
		info, err := os.Stat(e.Path)
		if err != nil || seen[key(e.Path)] {
			continue
		}
		e.Size = info.Size()
		out = append(out, e)
		seen[key(e.Path)] = true
	}
	dir := Dir(dataDir)
	if names, err := os.ReadDir(dir); err == nil {
		for _, d := range names {
			p := filepath.Join(dir, d.Name())
			if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".zip") || seen[key(p)] {
				continue
			}
			m, _, err := Read(p)
			if err != nil {
				continue
			}
			info, _ := d.Info()
			out = append(out, Entry{Path: p, Created: m.Created, Version: m.Version, Size: info.Size()})
			seen[key(p)] = true
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return b.Created.Compare(a.Created) })
	if len(out) != len(idx) {
		_ = writeIndex(dataDir, out)
	}
	return out
}

func key(path string) string { return strings.ToLower(filepath.Clean(path)) }

func readIndex(dataDir string) []Entry {
	var idx []Entry
	data, err := os.ReadFile(filepath.Join(Dir(dataDir), indexName))
	if err == nil {
		_ = json.Unmarshal(data, &idx)
	}
	return idx
}

func writeIndex(dataDir string, idx []Entry) error {
	if err := os.MkdirAll(Dir(dataDir), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(Dir(dataDir), indexName)
	if err := os.WriteFile(p+".tmp", data, 0o644); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func addToIndex(dataDir string, e Entry) error {
	idx := slices.DeleteFunc(readIndex(dataDir), func(x Entry) bool { return key(x.Path) == key(e.Path) })
	return writeIndex(dataDir, append(idx, e))
}
