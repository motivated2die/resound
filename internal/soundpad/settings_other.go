//go:build !windows

package soundpad

// ReadSettings finds nothing: Soundpad runs on Windows only.
func ReadSettings() (s Settings, recent []string) { return Settings{}, nil }
