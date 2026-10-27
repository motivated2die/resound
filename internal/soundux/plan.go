package soundux

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/motivated2die/resound/internal/audio"
)

// Plan is what an import would carry over, checked against the disk. The
// setup shows its counts as choices; the app applies the chosen parts.
type Plan struct {
	// Folders are the tabs' folders that exist, in Soundux's tab order,
	// without folders inside another one (scanned with it).
	Folders []Folder
	// MissingFolders are tab folders that are gone (moved, or a drive that
	// isn't connected).
	MissingFolders []string
	// Sounds are the per-sound settings of sounds whose files exist.
	Sounds []SoundSettings
	// MissingSounds counts sounds with settings whose file is gone.
	MissingSounds int
	// HotkeySounds counts sounds that had a hotkey. Resound has no keys per
	// sound (the overlay of favorites plays sounds by key), so the setup
	// only says they don't carry over.
	HotkeySounds int

	// PushToTalk and Stop are usable key combinations, nil if none.
	PushToTalk, Stop []uint16
	// DroppedKeys says why a combination wasn't taken, for the panel.
	DroppedKeys []string

	// MonitorVolume and CableVolume are Soundux's defaults on Resound's
	// curve (audio.FromLinear); nil when the file has none.
	MonitorVolume, CableVolume *int
	// NoSoundOverlap and MuteMic are nil when the file doesn't say.
	// Soundux's output devices aren't taken: the setup's audio step sets
	// up Resound's own (VB-Cable), which fits most users best.
	NoSoundOverlap, MuteMic *bool

	// Skipped counts unreadable tabs and sounds in the file.
	Skipped int
}

// Folder is one tab's folder.
type Folder struct {
	Path string
	Name string
	// Sort is the phone's sorting for the tab (library.Layout.Sort): "" is
	// Resound's default, newest first, as Soundux's default was.
	Sort string
	// OldestFirst: the tab was sorted oldest first, which the phone can't.
	OldestFirst bool
	Sounds      int // sounds Soundux listed whose files exist
	Covered     bool
}

// SoundSettings are one sound's settings to carry over.
type SoundSettings struct {
	Path                      string
	Favorite                  bool
	LocalVolume, RemoteVolume *int // Resound percent
}

// Disk is what Plan asks about files; tests replace it.
type Disk struct {
	IsDir  func(path string) bool
	IsFile func(path string) bool
}

// OS is the real disk.
var OS = Disk{
	IsDir: func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && fi.IsDir()
	},
	IsFile: func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && fi.Mode().IsRegular()
	},
}

// Plan checks f against the disk.
func (f *File) Plan(disk Disk) Plan {
	p := Plan{Skipped: f.Skipped}
	seen := map[string]bool{}
	for _, t := range f.Tabs {
		k := strings.ToLower(t.Path)
		if seen[k] {
			continue
		}
		seen[k] = true
		if !disk.IsDir(t.Path) {
			p.MissingFolders = append(p.MissingFolders, t.Path)
			continue
		}
		fo := Folder{Path: t.Path, Name: t.Name}
		switch t.SortMode {
		case SortNameAsc:
			fo.Sort = "name-asc"
		case SortNameDesc:
			fo.Sort = "name-desc"
		case SortDateAsc:
			fo.OldestFirst = true
		}
		for _, s := range t.Sounds {
			if !playable(s.Path) || !disk.IsFile(s.Path) {
				if s.Favorite || s.LocalVolume != nil || s.RemoteVolume != nil {
					p.MissingSounds++
				}
				continue
			}
			fo.Sounds++
			ss := SoundSettings{Path: s.Path, Favorite: s.Favorite,
				LocalVolume: volume(s.LocalVolume, 200), RemoteVolume: volume(s.RemoteVolume, 200)}
			if ss.Favorite || ss.LocalVolume != nil || ss.RemoteVolume != nil {
				p.Sounds = append(p.Sounds, ss)
			}
			if len(s.Hotkeys) > 0 {
				p.HotkeySounds++
			}
		}
		p.Folders = append(p.Folders, fo)
	}
	for i := range p.Folders {
		for j := range p.Folders {
			if i != j && within(p.Folders[i].Path, p.Folders[j].Path) {
				p.Folders[i].Covered = true
			}
		}
	}

	s := f.Settings
	p.PushToTalk = usableCombo(s.PushToTalkKeys, "push-to-talk", &p.DroppedKeys)
	p.Stop = usableCombo(s.StopHotkey, "stop", &p.DroppedKeys)
	if p.Stop != nil && p.PushToTalk != nil && sameKeys(p.Stop, p.PushToTalk) {
		p.DroppedKeys = append(p.DroppedKeys, "The stop key was also the push-to-talk key; only push-to-talk was kept.")
		p.Stop = nil
	}
	p.MonitorVolume = volume(s.LocalVolume, 100)
	p.CableVolume = volume(s.RemoteVolume, 100)
	if s.AllowOverlapping != nil {
		v := !*s.AllowOverlapping
		p.NoSoundOverlap = &v
	}
	p.MuteMic = s.MuteDuringPlayback
	return p
}

// Favorites counts the favorites to carry over.
func (p *Plan) Favorites() int {
	n := 0
	for _, s := range p.Sounds {
		if s.Favorite {
			n++
		}
	}
	return n
}

// Volumes counts the sounds with volumes of their own.
func (p *Plan) Volumes() int {
	n := 0
	for _, s := range p.Sounds {
		if s.LocalVolume != nil || s.RemoteVolume != nil {
			n++
		}
	}
	return n
}

// SoundCount is the number of sounds in the folders to carry over.
func (p *Plan) SoundCount() int {
	n := 0
	for _, f := range p.Folders {
		n += f.Sounds
	}
	return n
}

// Empty reports whether there is nothing to carry over.
func (p *Plan) Empty() bool {
	return len(p.Folders) == 0 && len(p.Sounds) == 0 && p.PushToTalk == nil && p.Stop == nil &&
		p.MonitorVolume == nil && p.CableVolume == nil && p.NoSoundOverlap == nil && p.MuteMic == nil
}

func playable(path string) bool {
	return slices.Contains(audio.SupportedExtensions, strings.ToLower(filepath.Ext(path)))
}

// volume converts a Soundux volume (linear percent) to Resound's curve.
func volume(v *int, most int) *int {
	if v == nil {
		return nil
	}
	r := audio.FromLinear(min(max(*v, 0), most))
	return &r
}

// usableCombo checks a combination for Resound: the left and right mouse
// buttons can't be bound (input.IsMouse). Conflicts with the keys Resound
// has already are the app's to check (keyConflict).
func usableCombo(c []uint16, what string, dropped *[]string) []uint16 {
	if len(c) == 0 {
		return nil
	}
	c = slices.Compact(slices.Clone(c))
	for _, vk := range c {
		if vk == 0x01 || vk == 0x02 {
			*dropped = append(*dropped, "The "+what+" key used the left or right mouse button, which Resound can't use.")
			return nil
		}
	}
	return c
}

func sameKeys(a, b []uint16) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) && rel != "."
}
