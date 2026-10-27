// Package soundux reads the settings of Soundux, the soundboard Resound
// replaces, so the guided setup can carry them over. Nothing here trusts the
// file: every field is read on its own (a wrong type drops that field, not
// the file, as Soundux's own reader does), keys and volumes are checked and
// converted, and Plan looks at the disk before anything is offered.
//
// The format is Soundux 0.2.7's (its last release): %APPDATA%\Soundux\
// config.json, {"data": {"tabs": [...]}, "settings": {...}}. Keys are Windows
// virtual-key codes; volumes are linear gains in percent, and a sound's own
// volume replaces the default rather than scaling it.
package soundux

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MaxSize is the largest config.json read. Soundux's grows by ~250 bytes per
// sound; 16 MB is far beyond any real library.
const MaxSize = 16 << 20

// Limits on what is taken from the file, so a damaged one can't flood the
// panel or the library.
const (
	maxTabs   = 500
	maxSounds = 50000
	maxKeys   = 6 // keys in one combination
)

// Sort modes of a Soundux tab (Enums::SortMode).
const (
	SortDateAsc = iota
	SortDateDesc
	SortNameAsc
	SortNameDesc
)

// File is what was read from Soundux's config.json.
type File struct {
	Tabs     []Tab
	Settings Settings
	// Skipped counts tabs and sounds dropped as unreadable.
	Skipped int
}

// Tab is one Soundux tab: a folder.
type Tab struct {
	Name     string
	Path     string
	SortMode int // -1 = not given
	Sounds   []Sound
}

// Sound is one sound of a tab.
type Sound struct {
	Name         string
	Path         string
	Favorite     bool
	Hotkeys      []uint16
	LocalVolume  *int // linear percent, nil = the default
	RemoteVolume *int
}

// Settings are Soundux's global settings Resound has a use for. Pointers are
// nil when the file doesn't have the field (or has it with a wrong type).
// Its output devices ("outputs") aren't read: the setup's audio step sets
// up the devices, and Soundux keeps no microphone (it used the default one).
type Settings struct {
	PushToTalkKeys     []uint16
	StopHotkey         []uint16
	LocalVolume        *int
	RemoteVolume       *int
	AllowOverlapping   *bool
	MuteDuringPlayback *bool
}

// Path is where Soundux keeps its config on Windows.
func Path() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Soundux", "config.json")
}

// Load reads and parses the config at path.
func Load(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSize {
		return nil, fmt.Errorf("%s is larger than %d MB", filepath.Base(path), MaxSize>>20)
	}
	return Parse(data)
}

// ErrNotSoundux is returned for JSON that isn't a Soundux config.
var ErrNotSoundux = errors.New("not a Soundux config")

type object map[string]json.RawMessage

// get decodes field key into dst; a missing field, null or a wrong type
// leaves dst alone and reports false.
func (o object) get(key string, dst any) bool {
	raw, ok := o[key]
	if !ok || string(raw) == "null" {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

// Parse reads a Soundux config.
func Parse(data []byte) (*File, error) {
	var top object
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSoundux, err)
	}
	var dataObj, settings object
	hasData, hasSettings := top.get("data", &dataObj), top.get("settings", &settings)
	if !hasData && !hasSettings {
		return nil, ErrNotSoundux
	}
	out := &File{}
	var tabs []json.RawMessage
	dataObj.get("tabs", &tabs)
	for _, raw := range tabs {
		if len(out.Tabs) == maxTabs {
			out.Skipped += len(tabs) - maxTabs
			break
		}
		tab, skipped, ok := parseTab(raw)
		out.Skipped += skipped
		if !ok {
			out.Skipped++
			continue
		}
		out.Tabs = append(out.Tabs, tab)
	}
	n := 0
	for i := range out.Tabs {
		if n+len(out.Tabs[i].Sounds) > maxSounds {
			out.Skipped += len(out.Tabs[i].Sounds) - (maxSounds - n)
			out.Tabs[i].Sounds = out.Tabs[i].Sounds[:maxSounds-n]
		}
		n += len(out.Tabs[i].Sounds)
	}
	s := &out.Settings
	s.PushToTalkKeys = keys(settings, "pushToTalkKeys")
	s.StopHotkey = keys(settings, "stopHotkey")
	s.LocalVolume = intField(settings, "localVolume")
	s.RemoteVolume = intField(settings, "remoteVolume")
	s.AllowOverlapping = boolField(settings, "allowOverlapping")
	s.MuteDuringPlayback = boolField(settings, "muteDuringPlayback")
	return out, nil
}

func parseTab(raw json.RawMessage) (Tab, int, bool) {
	var o object
	if json.Unmarshal(raw, &o) != nil {
		return Tab{}, 0, false
	}
	t := Tab{SortMode: -1}
	if !o.get("path", &t.Path) || !usablePath(t.Path) {
		return Tab{}, 0, false
	}
	t.Path = filepath.Clean(t.Path) // Soundux writes forward slashes
	o.get("name", &t.Name)
	if m := intField(o, "sortMode"); m != nil && *m >= SortDateAsc && *m <= SortNameDesc {
		t.SortMode = *m
	}
	var sounds []json.RawMessage
	o.get("sounds", &sounds)
	skipped := 0
	for _, sr := range sounds {
		var so object
		var snd Sound
		if json.Unmarshal(sr, &so) != nil || !so.get("path", &snd.Path) || !usablePath(snd.Path) {
			skipped++
			continue
		}
		snd.Path = filepath.Clean(snd.Path)
		so.get("name", &snd.Name)
		so.get("isFavorite", &snd.Favorite)
		snd.Hotkeys = keys(so, "hotkeys")
		snd.LocalVolume = intField(so, "localVolume")
		snd.RemoteVolume = intField(so, "remoteVolume")
		t.Sounds = append(t.Sounds, snd)
	}
	return t, skipped, true
}

// usablePath accepts absolute local or UNC paths of a sane length.
func usablePath(p string) bool {
	return p != "" && len(p) < 1024 && filepath.IsAbs(filepath.Clean(p))
}

// keys reads a key combination: virtual-key codes, or {"key": n} objects
// (a format some Soundux builds after 0.2.7 wrote). Codes outside 1-254
// make the whole combination unusable: without that key it is another one.
func keys(o object, field string) []uint16 {
	var raw []json.RawMessage
	if !o.get(field, &raw) || len(raw) == 0 || len(raw) > maxKeys {
		return nil
	}
	var out []uint16
	for _, r := range raw {
		var vk int
		if json.Unmarshal(r, &vk) != nil {
			var k struct {
				Key  *int `json:"key"`
				Type int  `json:"type"`
			}
			if json.Unmarshal(r, &k) != nil || k.Key == nil || k.Type != 0 {
				return nil // a mouse or MIDI key of a newer format
			}
			vk = *k.Key
		}
		if vk < 1 || vk > 254 {
			return nil
		}
		out = append(out, uint16(vk))
	}
	return out
}

func intField(o object, key string) *int {
	var f float64 // tolerate 50.0
	if !o.get(key, &f) || f != float64(int(f)) {
		return nil
	}
	v := int(f)
	return &v
}

func boolField(o object, key string) *bool {
	var b bool
	if !o.get(key, &b) {
		return nil
	}
	return &b
}
