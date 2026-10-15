package library

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func tabNames(tabs []Tab) string {
	var names []string
	for _, t := range tabs {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func tabIDs(t *testing.T, l *Library, names ...string) []uint64 {
	t.Helper()
	var ids []uint64
	for _, n := range names {
		i := slices.IndexFunc(l.Tabs(), func(tab Tab) bool { return tab.Name == n })
		if i < 0 {
			t.Fatalf("no tab %q in %s", n, tabNames(l.Tabs()))
		}
		ids = append(ids, l.Tabs()[i].ID)
	}
	return ids
}

func wantPhone(t *testing.T, l *Library, want string) {
	t.Helper()
	if got := tabNames(l.PhoneTabs()); got != want {
		t.Fatalf("phone tabs = %s, want %s", got, want)
	}
}

// The phone's arrangement applies to PhoneTabs only, survives restarts, and
// adapts to folders that come, go and come back.
func TestTabOrder(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b", "c"} {
		touch(t, filepath.Join(root, d, d+".mp3"), time.Now())
	}
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	wantPhone(t, lib, "a, b, c") // no arrangement: folder order

	shown, err := lib.SetTabOrder(tabIDs(t, lib, "c", "a", "b"))
	if err != nil || !slices.Equal(shown, tabIDs(t, lib, "c", "a", "b")) {
		t.Fatalf("SetTabOrder = %v, %v", shown, err)
	}
	wantPhone(t, lib, "c, a, b")
	if got := tabNames(lib.Tabs()); got != "a, b, c" {
		t.Fatalf("panel tabs = %s, want folder order", got)
	}
	lib = reopen(t, lib)
	lib.Scan([]string{root})
	wantPhone(t, lib, "c, a, b")

	// New folders go after the tab before them in folder order: a nested
	// one next to its parent, a new sibling after its neighbour, and one
	// that comes first in folder order first.
	touch(t, filepath.Join(root, "a", "x", "x.mp3"), time.Now())
	touch(t, filepath.Join(root, "b2", "b2.mp3"), time.Now())
	touch(t, filepath.Join(root, "0", "0.mp3"), time.Now())
	lib.Scan([]string{root})
	wantPhone(t, lib, "0, c, a, a › x, b, b2")

	// A removed folder leaves the bar but keeps its place, also when the
	// phone saves an arrangement meanwhile, and returns there.
	bTab := tabIDs(t, lib, "b")[0]
	if err := os.RemoveAll(filepath.Join(root, "b")); err != nil {
		t.Fatal(err)
	}
	lib.Scan([]string{root})
	wantPhone(t, lib, "0, c, a, a › x, b2")
	if _, err := lib.SetTabOrder(tabIDs(t, lib, "0", "a", "a › x", "c", "b2")); err != nil {
		t.Fatal(err)
	}
	wantPhone(t, lib, "0, a, a › x, c, b2")
	touch(t, filepath.Join(root, "b", "b.mp3"), time.Now())
	lib.Scan([]string{root})
	if tabIDs(t, lib, "b")[0] != bTab {
		t.Fatal("re-added folder got a new tab ID")
	}
	wantPhone(t, lib, "0, a, a › x, b, c, b2")

	// A phone that hadn't seen a new tab yet moves another: the new tab
	// stays after the tab it followed. Unknown and repeated IDs are ignored.
	touch(t, filepath.Join(root, "d", "d.mp3"), time.Now())
	lib.Scan([]string{root})
	wantPhone(t, lib, "0, a, a › x, b, c, d, b2")
	ids := append(tabIDs(t, lib, "b2", "0", "a", "a › x", "b", "c", "a"), 12345)
	if _, err := lib.SetTabOrder(ids); err != nil {
		t.Fatal(err)
	}
	wantPhone(t, lib, "b2, 0, a, a › x, b, c, d")

	// Resetting the phone's appearance returns to folder order.
	if err := lib.ResetAppearance(); err != nil {
		t.Fatal(err)
	}
	wantPhone(t, lib, "0, a, a › x, b, b2, c, d")
}

// Expired tab tombstones leave the stored order.
func TestTabOrderForgetsExpiredTabs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b"} {
		touch(t, filepath.Join(root, d, d+".mp3"), time.Now())
	}
	lib := open(t, t.TempDir())
	lib.Scan([]string{root})
	lib.SetTabOrder(tabIDs(t, lib, "b", "a"))
	if err := os.RemoveAll(filepath.Join(root, "b")); err != nil {
		t.Fatal(err)
	}
	lib.Scan([]string{root})
	if len(lib.tabOrder) != 2 {
		t.Fatalf("tombstoned tab left the order early: %v", lib.tabOrder)
	}
	now := time.Now()
	lib.now = func() time.Time { return now.Add(TombstoneTTL + time.Hour) }
	lib.Scan([]string{root})
	if !slices.Equal(lib.tabOrder, tabIDs(t, lib, "a")) {
		t.Fatalf("order after expiry = %v", lib.tabOrder)
	}
}

func TestMergeAfter(t *testing.T) {
	all := func(uint64) bool { return true }
	for _, c := range []struct {
		base, ref, want []uint64
	}{
		{nil, []uint64{1, 2}, []uint64{1, 2}},
		{[]uint64{3, 1}, []uint64{1, 2, 3}, []uint64{3, 1, 2}},
		{[]uint64{3, 1}, []uint64{0, 1, 2, 4, 3, 5}, []uint64{0, 3, 5, 1, 2, 4}},
		{[]uint64{2, 1}, []uint64{1, 2}, []uint64{2, 1}},
	} {
		if got := mergeAfter(c.base, c.ref, all); !slices.Equal(got, c.want) {
			t.Errorf("mergeAfter(%v, %v) = %v, want %v", c.base, c.ref, got, c.want)
		}
	}
	if got := mergeAfter([]uint64{1}, []uint64{1, 2, 3}, func(id uint64) bool { return id != 2 }); !slices.Equal(got, []uint64{1, 3}) {
		t.Errorf("filtered: %v", got)
	}
}
