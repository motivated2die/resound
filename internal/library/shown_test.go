package library

import (
	"slices"
	"testing"
)

// ShowOrder arranges sounds as the phone does (app.js orderSounds): the
// custom order of the tab's layout mode first, the others after it as the
// PC lists them, then sorted by name if the tab is sorted.
func TestShowOrder(t *testing.T) {
	sounds := []Sound{{ID: 1, Name: "clip 10"}, {ID: 2, Name: "Airhorn"}, {ID: 3, Name: "Clip 2"}, {ID: 4, Name: "bruh"}}
	ids := func(s []Sound) []uint64 {
		out := make([]uint64, len(s))
		for i, snd := range s {
			out[i] = snd.ID
		}
		return out
	}
	cases := []struct {
		name string
		lay  Layout
		want []uint64
	}{
		{"no layout: as listed", Layout{}, []uint64{1, 2, 3, 4}},
		{"the default mode's order, the rest after it",
			Layout{Orders: map[string][]uint64{DefaultMode: {3, 1}}}, []uint64{3, 1, 2, 4}},
		{"only the current mode's order counts",
			Layout{Mode: "list", Orders: map[string][]uint64{DefaultMode: {3, 1}, "list": {4, 9, 4}}}, []uint64{4, 1, 2, 3}},
		{"A–Z", Layout{Sort: SortNameAsc, Orders: map[string][]uint64{DefaultMode: {3, 1}}}, []uint64{2, 4, 3, 1}},
		{"Z–A", Layout{Sort: SortNameDesc}, []uint64{1, 3, 4, 2}},
	}
	for _, c := range cases {
		if got := ids(ShowOrder(sounds, c.lay)); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if sounds[0].ID != 1 || sounds[3].ID != 4 {
		t.Fatal("ShowOrder changed its argument")
	}
}
