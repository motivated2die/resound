package library

import (
	"slices"
	"testing"
)

func TestCompareNames(t *testing.T) {
	names := []string{"clip 10", "Airhorn", "Clip 2", "bruh", "clip 02b", "Ähm", "clip", "10 seconds", "9 lives"}
	slices.SortStableFunc(names, CompareNames)
	want := []string{"9 lives", "10 seconds", "Ähm", "Airhorn", "bruh", "clip", "Clip 2", "clip 02b", "clip 10"}
	if !slices.Equal(names, want) {
		t.Fatalf("sorted:\n got %q\nwant %q", names, want)
	}
	if CompareNames("Bruh", "bruh") != 0 || CompareNames("a", "b") >= 0 || CompareNames("b", "a") <= 0 {
		t.Fatal("case or order")
	}
}
