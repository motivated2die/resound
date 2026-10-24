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
// IDs (stable across renames). Each chosen device's friendly name is kept
// too (the *DeviceName fields): some virtual devices, like Voicemeeter's,
// get new IDs at every Windows start, and their name then finds them again.
type Config struct {
	// MonitorDeviceID is the output the user hears. "" = system default.
	MonitorDeviceID   string `json:"monitorDeviceId"`
	MonitorDeviceName string `json:"monitorDeviceName,omitempty"`
	// CableDeviceID is the output others hear. "" = automatic: VB-Cable's
	// "CABLE Input" when it is installed, else none. Another device is an
	// advanced choice, e.g. a virtual mixer's input.
	CableDeviceID   string `json:"cableDeviceId"`
	CableDeviceName string `json:"cableDeviceName,omitempty"`
	// AppsMicDeviceID is, with such an own routing, the recording device
	// others' apps use as their microphone (e.g. "Voicemeeter Out B1"),
	// where the user's mixer combines the voice and the sounds. Resound
	// only shows it and measures it. Cleared when going back to VB-Cable.
	AppsMicDeviceID   string `json:"appsMicDeviceId,omitempty"`
	AppsMicDeviceName string `json:"appsMicDeviceName,omitempty"`
	// Volumes in percent, 0-100, on the loudness curve (audio.Gain).
	MonitorVolume int `json:"monitorVolume"`
	CableVolume   int `json:"cableVolume"`
	// VolumeScale is the scale the volumes were saved on: 0 = the linear
	// scale of versions before 0.9 (converted once at start), 1 = the curve.
	VolumeScale int `json:"volumeScale,omitempty"`
	// MicDeviceID is the user's real microphone (capture endpoint ID).
	// "" = pick the default recording device automatically.
	MicDeviceID   string `json:"micDeviceId"`
	MicDeviceName string `json:"micDeviceName,omitempty"`
	// MuteMicWhilePlaying mutes the microphone while sounds play, so the
	// voice does not mix with the sound on the cable. Off for new configs
	// since 0.13 (it confused many); saved configs keep their choice.
	MuteMicWhilePlaying bool `json:"muteMicWhilePlaying"`
	// MuteMicMinSeconds: with MuteMicWhilePlaying, the mic is muted only
	// while a sound longer than this plays (or one of unknown length);
	// shorter ones leave it open. 0 = for every sound. Configs from before
	// it get the default, 5.
	MuteMicMinSeconds float64 `json:"muteMicMinSeconds"`
	// PushToTalkKeys is the game's push-to-talk key combination (Windows
	// virtual-key codes; 4-6 are mouse buttons 3-5). Empty = not used.
	PushToTalkKeys []uint16 `json:"pushToTalkKeys"`
	// PushToTalkExtra are further push-to-talk combinations, held together
	// with PushToTalkKeys, e.g. a game's and Discord's. Only while
	// PushToTalkKeys is set (versions before it read that one alone).
	PushToTalkExtra [][]uint16 `json:"pushToTalkExtra,omitempty"`
	// PushToTalkWhilePlaying holds the push-to-talk keys while sounds play.
	// Talk-through always holds them.
	PushToTalkWhilePlaying bool `json:"pushToTalkWhilePlaying"`
	// Folders are the sound folders; each becomes tabs on the phone.
	Folders []string `json:"folders"`
	// ServerPort is the UDP port the app connects to (the name is from the
	// HTTP server of earlier versions; same default).
	ServerPort int `json:"serverPort"`
	// AppURL replaces the app's address (app.AppURL), e.g. for
	// a test deployment. Usually empty.
	AppURL string `json:"appURL,omitempty"`
	// TestSoundPath is the file played by "Play test sound" (diagnostics).
	TestSoundPath string `json:"testSoundPath"`
	// SetupComplete is set once the guided first-run setup was finished or
	// skipped; until then the panel opens on the setup.
	SetupComplete bool `json:"setupComplete"`
	// SetupStep is the guided setup's current step while it isn't
	// complete, so it continues there after a restart (e.g. the one
	// VB-CABLE's install needs). 0 = the welcome page.
	SetupStep int `json:"setupStep,omitempty"`
	// SetupFlow is the numbering SetupStep uses (app.setupFlow); 0 is the
	// five-step setup of earlier versions, migrated once at start.
	SetupFlow int `json:"setupFlow,omitempty"`
	// PauseHotkeys are key combinations that, while held anywhere in
	// Windows, pause playback and let go of push-to-talk and the mic.
	// StopHotkeys stop all sounds when pressed. Each is a list of
	// virtual-key combinations, like PushToTalkKeys.
	PauseHotkeys [][]uint16 `json:"pauseHotkeys,omitempty"`
	StopHotkeys  [][]uint16 `json:"stopHotkeys,omitempty"`
	// AppKeybinds are programs with keybinds of their own: while one is in
	// the foreground, each of its overrides replaces the global keys above
	// for that function; functions without one keep the global keys.
	AppKeybinds []AppKeybinds `json:"appKeybinds,omitempty"`
	// NoPushToTalkHandOver passes pause and stop keys on at once, even while
	// sounds hold push-to-talk. By default the key waits for Resound to let
	// go of push-to-talk first (input.Watcher.LendPTT).
	NoPushToTalkHandOver bool `json:"noPushToTalkHandOver,omitempty"`
	// NoPushToTalkPauses: the user's own presses of the push-to-talk keys
	// Resound holds do nothing special. By default (false) holding one pauses
	// the sounds like a pause key, and Resound presses push-to-talk again
	// after it is let go (app.onTalk): the user's release has ended it in the
	// game.
	NoPushToTalkPauses bool `json:"noPushToTalkPauses,omitempty"`
	// NoUpdateCheck turns off the automatic check for new releases on
	// GitHub ("Check now" still works).
	NoUpdateCheck bool `json:"noUpdateCheck,omitempty"`
	// NoNormalize plays sounds at their own loudness instead of evening
	// them out (audio.NormGain).
	NoNormalize bool `json:"noNormalize,omitempty"`
	// OverlayKeys opens the in-game overlay of favorites (a key
	// combination, like PushToTalkKeys). Empty = no overlay.
	OverlayKeys []uint16 `json:"overlayKeys,omitempty"`
	// OverlayRight puts the overlay at the right screen edge (else the left).
	OverlayRight bool `json:"overlayRight,omitempty"`
	// OverlayLast is the sound last highlighted in the overlay, highlighted
	// again when it opens. 0 = the middle one.
	OverlayLast uint64 `json:"overlayLast,omitempty"`
	// PanelPos is where the user moved the panel by its tab: its top left
	// corner in screen pixels. nil = docked next to the tray icon. It is
	// only used while that point is on a screen, kept inside its work area.
	PanelPos *PanelPos `json:"panelPos,omitempty"`
}

// AppKeybinds are one program's keybinds. A nil override keeps the global
// keys for that function.
type AppKeybinds struct {
	// Exe is the program's file name in lower case, e.g. "valorant.exe"
	// (input.AppName), the same across updates and install folders.
	Exe string `json:"exe"`
	// Name is shown in the panel, e.g. "VALORANT".
	Name       string       `json:"name,omitempty"`
	PushToTalk *KeyOverride `json:"pushToTalk,omitempty"`
	Pause      *KeyOverride `json:"pause,omitempty"`
	Stop       *KeyOverride `json:"stop,omitempty"`
}

// KeyOverride replaces a global keybind in one program: with key
// combinations of its own, or with none at all (Off), which turns the
// function off there.
type KeyOverride struct {
	Off  bool       `json:"off,omitempty"`
	Keys [][]uint16 `json:"keys,omitempty"`
}

// PanelPos is a position on the virtual screen, in pixels.
type PanelPos struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// Default returns the configuration used on first start.
func Default() Config {
	return Config{MonitorVolume: 80, CableVolume: 100, VolumeScale: 1, ServerPort: 8080, PushToTalkWhilePlaying: true, MuteMicMinSeconds: 5}
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
	cfg.VolumeScale = 0 // a saved config without it is on the old linear scale
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default()
	}
	if cfg.ServerPort <= 0 || cfg.ServerPort > 65535 {
		cfg.ServerPort = 8080
	}
	if cfg.MuteMicMinSeconds < 1 { // below a second reads "always"
		cfg.MuteMicMinSeconds = 0
	}
	cfg.MuteMicMinSeconds = min(cfg.MuteMicMinSeconds, 600)
	cfg.cleanKeybinds()
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
