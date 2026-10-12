package vbcable

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Limits for the driver pack (about 3.5 MB unpacked in 31 files).
const (
	maxPackFiles     = 200
	maxPackFileBytes = 32 << 20
	maxPackBytes     = 64 << 20
)

// extract unpacks the driver pack into dest. The pack is flat, so entries
// with folders, drive letters or ".." are rejected rather than followed.
func extract(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening the driver pack: %w", err)
	}
	defer r.Close()
	if len(r.File) > maxPackFiles {
		return fmt.Errorf("driver pack has %d files", len(r.File))
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	var total int64
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !safeName(f.Name) {
			return fmt.Errorf("driver pack has an unexpected entry %q", f.Name)
		}
		n, err := extractFile(f, filepath.Join(dest, f.Name))
		if err != nil {
			return err
		}
		if total += n; total > maxPackBytes {
			return errors.New("driver pack is larger than expected")
		}
	}
	return nil
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\:`) && filepath.Base(name) == name
}

func extractFile(f *zip.File, path string) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", f.Name, err)
	}
	defer rc.Close()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(rc, maxPackFileBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxPackFileBytes {
		err = fmt.Errorf("%s is larger than expected", f.Name)
	}
	if err != nil {
		return n, fmt.Errorf("unpacking %s: %w", f.Name, err)
	}
	return n, nil
}
