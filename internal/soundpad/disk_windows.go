package soundpad

import "golang.org/x/sys/windows"

// FreeSpace is the room on dir's drive for this user, false when it can't
// be read.
func FreeSpace(dir string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, false
	}
	return free, true
}
