//go:build !windows

package soundpad

// ReadSettings finds nothing: Soundpad runs on Windows only.
func ReadSettings() (s Settings, recent []string) { return Settings{}, nil }

// FreeSpace isn't known off Windows.
func FreeSpace(dir string) (uint64, bool) { return 0, false }
