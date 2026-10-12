// Package config loads and saves the persistent host settings.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AppName is used for folders, window classes and the mutex name.
const AppName = "Resound"

// Config is the persisted host configuration. Device IDs are WASAPI endpoint
// IDs (stable across renames), never friendly names.
type Config struct {
	// MonitorDeviceID is the output the user hears. "" = system default.
	MonitorDeviceID string `json:"monitorDeviceId"`
	// CableDeviceID is the output others hear (e.g. "CABLE Input"). "" = off.
	CableDeviceID string `json:"cableDeviceId"`
	// Volumes in percent, 0-100.
	MonitorVolume int `json:"monitorVolume"`
	CableVolume   int `json:"cableVolume"`
	// MicDeviceID is the user's real microphone (capture endpoint ID).
	// "" = pick the default recording device automatically.
	MicDeviceID string `json:"micDeviceId"`
	// MuteMicWhilePlaying mutes the microphone while sounds play, so the
	// voice does not mix with the sound on the cable.
	MuteMicWhilePlaying bool `json:"muteMicWhilePlaying"`
	// PushToTalkKeys is the game's push-to-talk key combination (Windows
	// virtual-key codes; 4-6 are mouse buttons 3-5). Empty = not used.
	PushToTalkKeys []uint16 `json:"pushToTalkKeys"`
	// PushToTalkWhilePlaying holds the push-to-talk keys while sounds play.
	// Talk-through always holds them.
	PushToTalkWhilePlaying bool `json:"pushToTalkWhilePlaying"`
	// Folders are the sound folders; each becomes tabs on the phone.
	Folders []string `json:"folders"`
	// ServerPort is the phone remote's HTTP port.
	ServerPort int `json:"serverPort"`
	// TestSoundPath is the file played by "Play test sound" (diagnostics).
	TestSoundPath string `json:"testSoundPath"`
	// SetupComplete is set once the guided first-run setup was finished or
	// skipped; until then the panel opens on the setup.
	SetupComplete bool `json:"setupComplete"`
	// SetupStep is the guided setup's current step while it isn't
	// complete, so it continues there after a restart (e.g. the one
	// VB-CABLE's install needs). 0 = the welcome page.
	SetupStep int `json:"setupStep,omitempty"`
}

// Default returns the configuration used on first start.
func Default() Config {
	return Config{MonitorVolume: 50, CableVolume: 100, ServerPort: 8080, PushToTalkWhilePlaying: true, MuteMicWhilePlaying: true}
}

// Dir returns %LOCALAPPDATA%\Resound.
func Dir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, AppName)
}

func path() string { return filepath.Join(Dir(), "config.json") }

// Load reads the config, falling back to defaults for a missing or broken file.
func Load() Config {
	cfg := Default()
	data, err := os.ReadFile(path())
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default()
	}
	if cfg.ServerPort <= 0 || cfg.ServerPort > 65535 {
		cfg.ServerPort = 8080
	}
	cfg.MonitorVolume = clamp(cfg.MonitorVolume)
	cfg.CableVolume = clamp(cfg.CableVolume)
	return cfg
}

// Save writes the config atomically (temp file + rename).
func Save(cfg Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path())
}

func clamp(v int) int {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	}
	return v
}
