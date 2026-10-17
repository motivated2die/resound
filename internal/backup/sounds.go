package backup

// Sound zips: a copy of the user's sound folders in one zip file, for
// keeping or moving them. They are not settings backups: the app never
// lists or restores them, and they are saved wherever the user chooses.

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SoundFolder is one library folder to zip, as the library scanned it.
type SoundFolder struct {
	Root    string   // the library folder
	Folders []string // its subfolders at any depth, empty ones included
	Files   []string // its sound files at any depth
}

// SoundsResult describes a written sound zip.
type SoundsResult struct {
	Path    string
	Size    int64 // of the zip file
	Files   int   // sound files in it
	Skipped int   // files that vanished or couldn't be read while zipping
}

// SoundsName names a sound zip after its time, e.g.
// "Resound-sounds-2026-10-12_13-05-09.zip".
func SoundsName(t time.Time) string {
	return "Resound-sounds-" + t.Format("2006-01-02_15-04-05") + ".zip"
}

// soundsSize returns the number and total size of the folders' files that
// exist, for the progress.
func soundsSize(folders []SoundFolder) (files int, size int64) {
	for _, f := range folders {
		for _, p := range f.Files {
			if info, err := os.Stat(p); err == nil {
				files++
				size += info.Size()
			}
		}
	}
	return files, size
}

// ZipSounds writes folders to dest (a .zip path). Each library folder
// becomes a folder at the top of the zip, named like it ("Sounds", or
// "Sounds (2)" for a second folder of that name), with its subfolders and
// files at their places below it. Every folder gets its own entry, so empty
// ones are kept. Already compressed formats (MP3, OGG, FLAC) are stored as
// they are; others are deflated.
//
// The zip is written next to dest under a temporary name and renamed when
// complete, so dest never holds a partial zip. progress, if set, gets the
// bytes copied so far and the total, from the same goroutine. Files that
// vanished or can't be read are skipped and counted; cancelling ctx stops
// and removes the temporary file.
func ZipSounds(ctx context.Context, dest string, folders []SoundFolder, progress func(done, total int64)) (SoundsResult, error) {
	res := SoundsResult{Path: dest}
	_, total := soundsSize(folders)
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return res, err
	}
	w := &zipWork{ctx: ctx, zw: zip.NewWriter(f), total: total, progress: progress}
	used := map[string]bool{}
	for _, folder := range folders {
		if err = w.addFolder(folder, topName(folder.Root, used), &res); err != nil {
			break
		}
	}
	if err == nil {
		err = w.zw.Close()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		os.Remove(tmp)
		return res, err
	}
	if info, err := os.Stat(dest); err == nil {
		res.Size = info.Size()
	}
	return res, nil
}

// topName is the zip folder for library folder root: its name, made unique
// (case-insensitively, as Windows compares) with " (2)", " (3)", …
func topName(root string, used map[string]bool) string {
	base := filepath.Base(filepath.Clean(root))
	if base == "" || base == "." || strings.ContainsAny(base, `\/:`) { // a drive root, e.g. D:\
		base = strings.TrimSuffix(filepath.VolumeName(root), ":")
	}
	if base == "" {
		base = "Sounds"
	}
	name := base
	for n := 2; used[strings.ToLower(name)]; n++ {
		name = fmt.Sprintf("%s (%d)", base, n)
	}
	used[strings.ToLower(name)] = true
	return name
}

type zipWork struct {
	ctx         context.Context
	zw          *zip.Writer
	done, total int64
	progress    func(done, total int64)
}

func (w *zipWork) addFolder(folder SoundFolder, top string, res *SoundsResult) error {
	if err := w.addDir(top+"/", folder.Root); err != nil {
		return err
	}
	for _, dir := range folder.Folders {
		if rel, ok := relSlash(folder.Root, dir); ok {
			if err := w.addDir(top+"/"+rel+"/", dir); err != nil {
				return err
			}
		}
	}
	for _, p := range folder.Files {
		rel, ok := relSlash(folder.Root, p)
		if !ok {
			continue
		}
		switch err := w.addFile(top+"/"+rel, p); {
		case err == nil:
			res.Files++
		case w.ctx.Err() != nil:
			return w.ctx.Err()
		case errors.Is(err, errUnreadable):
			res.Skipped++
		default:
			return err
		}
	}
	return w.ctx.Err()
}

// relSlash is path below root with forward slashes, as zip names have them;
// ok is false for anything not below root.
func relSlash(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (w *zipWork) addDir(name, path string) error {
	h := &zip.FileHeader{Name: name, Modified: time.Now()}
	if info, err := os.Stat(path); err == nil {
		h.Modified = info.ModTime()
	}
	h.SetMode(os.ModeDir | 0o755)
	_, err := w.zw.CreateHeader(h)
	return err
}

// errUnreadable marks a sound file that couldn't be opened; it is skipped.
var errUnreadable = errors.New("unreadable")

func (w *zipWork) addFile(name, path string) error {
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %v", errUnreadable, err)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", errUnreadable, path)
	}
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	h.Name = name
	h.Method = zip.Deflate
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".ogg", ".flac", ".opus", ".m4a", ".aac":
		h.Method = zip.Store // compressed already: deflating costs time and saves nothing
	}
	dst, err := w.zw.CreateHeader(h)
	if err != nil {
		return err
	}
	// A read error halfway can't be skipped: the entry is started. It fails
	// the zip, like a write error.
	_, err = io.Copy(dst, &progressReader{w: w, r: src})
	return err
}

// progressReader counts what is copied and stops when the zip is cancelled.
type progressReader struct {
	w *zipWork
	r io.Reader
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	p.w.done += int64(n)
	if n > 0 && p.w.progress != nil {
		p.w.progress(p.w.done, p.w.total)
	}
	return n, err
}
