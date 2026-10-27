package soundpad

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Copying sounds by category: a folder per category (PlanCategory.Dir) in a
// folder the user chose, each sound copied into the folders of its
// categories, once per folder. The originals stay where they are.
//
// A file is written under a "~resound-" name, which the library skips, and
// renamed when complete, so a stopped copy leaves no half a sound. A file of
// the same name and size in a category's folder counts as copied already
// (a copy run again); another one of the same name gets " (2)".

// Copied is where a copy by category put the sounds.
type Copied struct {
	// Paths has, per Plan.Categories, the copy of each of its Sounds, ""
	// where the original couldn't be read.
	Paths [][]string
	// Copied counts files written, Existing those that were there already,
	// Failed originals that couldn't be read.
	Copied, Existing, Failed int
}

// ErrStopped is returned when stop asked a copy to end.
var ErrStopped = errors.New("copying stopped")

// CopyByCategory copies p's sounds into a folder per category below target.
// progress gets the number of files done so far after each; stop is asked
// before each and ends the copy with ErrStopped. Other errors are writing
// ones (no room, no permission); what was copied before stays.
func CopyByCategory(target string, p *Plan, progress func(done int), stop func() bool) (Copied, error) {
	var c Copied
	done := 0
	for _, cat := range p.Categories {
		dir := filepath.Join(target, cat.Dir)
		paths := make([]string, len(cat.Sounds))
		c.Paths = append(c.Paths, paths)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return c, err
		}
		used := map[string]bool{} // names taken in dir by this copy, lower case
		for k, i := range cat.Sounds {
			if stop() {
				return c, ErrStopped
			}
			src := p.Sounds[i].Path
			dst, exists := destination(dir, filepath.Base(src), p.Sizes[i], used)
			switch {
			case exists:
				c.Existing++
				paths[k] = dst
			default:
				err := copyFile(src, dst)
				var read readError
				switch {
				case errors.As(err, &read):
					c.Failed++
					delete(used, strings.ToLower(filepath.Base(dst)))
				case err != nil:
					return c, err
				default:
					c.Copied++
					paths[k] = dst
				}
			}
			done++
			progress(done)
		}
	}
	return c, nil
}

// destination is where a file named name of size goes in dir: name, or
// "name (2)" and on when that is taken by this copy or by another file.
// exists reports a file of that name and size there, taken as copied
// already.
func destination(dir, name string, size int64, used map[string]bool) (path string, exists bool) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		try := name
		if i > 1 {
			try = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		if used[strings.ToLower(try)] {
			continue
		}
		p := filepath.Join(dir, try)
		info, err := os.Lstat(p)
		if err == nil && (!info.Mode().IsRegular() || info.Size() != size) {
			continue // another file of that name
		}
		used[strings.ToLower(try)] = true
		return p, err == nil
	}
}

// readError is an original that couldn't be read: skipped, not fatal.
type readError struct{ error }

// copyFile copies src to dst through a temporary file in dst's folder and
// keeps src's modification time.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return readError{err}
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return readError{err}
	}
	var rnd [6]byte
	rand.Read(rnd[:])
	tmp := filepath.Join(filepath.Dir(dst), "~resound-"+hex.EncodeToString(rnd[:])+filepath.Ext(dst))
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			out.Close()
			os.Remove(tmp)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) && pe.Path == src {
			return readError{err}
		}
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	os.Chtimes(tmp, info.ModTime(), info.ModTime())
	return os.Rename(tmp, dst)
}
