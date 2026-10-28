package library

import (
	"slices"
	"strconv"
)

// The phone shows a tab's sounds in its custom order for the tab's layout
// mode, sounds the order doesn't hold after them as the PC lists them, or
// sorted by name when the tab is set to that (webapp app.js orderSounds,
// sort.js). The PC reproduces that order for what it shows itself, the
// in-game overlay's favorites, and for the launchpad's keys, which follow
// the buttons as they stand on the phone.

// DefaultMode is the phone's layout mode for a tab without one.
const DefaultMode = "grid-3"

// ShownSounds returns the sounds of a tab (its ID as a string) or of the
// favorites (FavoritesKey), in the order the phone shows them. ok is false
// for a tab that doesn't exist.
func (l *Library) ShownSounds(key string) (sounds []Sound, ok bool) {
	if key == FavoritesKey {
		sounds = l.Favorites()
	} else {
		id, err := strconv.ParseUint(key, 10, 64)
		if err != nil {
			return nil, false
		}
		tab, found := l.Tab(id)
		if !found {
			return nil, false
		}
		sounds = tab.Sounds
	}
	return ShowOrder(sounds, l.Layout(key)), true
}

// ShowOrder arranges sounds, as the PC lists them, the way the phone shows
// them with layout lay.
func ShowOrder(sounds []Sound, lay Layout) []Sound {
	mode := lay.Mode
	if mode == "" {
		mode = DefaultMode
	}
	pos := map[uint64]int{}
	for i, id := range lay.Orders[mode] {
		if _, dup := pos[id]; !dup {
			pos[id] = i
		}
	}
	out := slices.Clone(sounds)
	slices.SortStableFunc(out, func(x, y Sound) int {
		px, okx := pos[x.ID]
		py, oky := pos[y.ID]
		switch {
		case okx && oky:
			return px - py
		case okx:
			return -1
		case oky:
			return 1
		}
		return 0
	})
	switch lay.Sort {
	case SortNameAsc:
		slices.SortStableFunc(out, func(x, y Sound) int { return CompareNames(x.Name, y.Name) })
	case SortNameDesc:
		slices.SortStableFunc(out, func(x, y Sound) int { return CompareNames(y.Name, x.Name) })
	}
	return out
}
