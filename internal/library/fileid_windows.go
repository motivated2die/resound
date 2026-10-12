package library

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// fileID returns the NTFS identity of a file: volume serial number plus file
// index. It survives renames and moves within a volume, so a scan can match
// a renamed file to its record without reading it. "" when unavailable.
func fileID(path string) string {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var fi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &fi); err != nil {
		return ""
	}
	if fi.FileIndexHigh == 0 && fi.FileIndexLow == 0 {
		return "" // file systems without stable indexes (some network drives)
	}
	return fmt.Sprintf("%08x:%08x%08x", fi.VolumeSerialNumber, fi.FileIndexHigh, fi.FileIndexLow)
}
