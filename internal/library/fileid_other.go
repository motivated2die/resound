//go:build !windows

package library

import (
	"fmt"
	"os"
	"syscall"
)

// fileID returns the device and inode number of a file (the counterpart of
// the NTFS file index, used when running the tests on other systems).
func fileID(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%x:%x", uint64(st.Dev), uint64(st.Ino))
}
