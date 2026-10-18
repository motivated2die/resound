package app

import (
	"testing"
	"time"
)

// A sound mutes the mic when it is longer than the threshold, when its
// length is unknown, or always with a threshold of 0. Equal isn't longer.
func TestMuteFor(t *testing.T) {
	for _, tc := range []struct {
		length time.Duration
		min    float64
		want   bool
	}{
		{6 * time.Second, 5, true},
		{5 * time.Second, 5, false},
		{4900 * time.Millisecond, 5, false},
		{5100 * time.Millisecond, 5, true},
		{0, 5, true}, // unknown length: muted, as before there was a threshold
		{300 * time.Millisecond, 0, true},
		{2 * time.Second, 1, true},
		{800 * time.Millisecond, 1, false},
	} {
		if got := muteFor(tc.length, tc.min); got != tc.want {
			t.Errorf("muteFor(%v, %v) = %v, want %v", tc.length, tc.min, got, tc.want)
		}
	}
}

func TestMuteSteps(t *testing.T) {
	for _, tc := range []struct{ v, down, up float64 }{
		{5, 4, 6},
		{1, 0, 2},       // below one second reads "Always"
		{0, 0, 1},       // "Always" is the bottom
		{7.5, 7, 8},     // a typed value between steps
		{600, 300, 600}, // the top
		{13, 12, 15},
	} {
		if d, u := muteStepDown(tc.v), muteStepUp(tc.v); d != tc.down || u != tc.up {
			t.Errorf("steps from %v = %v, %v; want %v, %v", tc.v, d, u, tc.down, tc.up)
		}
	}
	for v, want := range map[float64]string{0: "Always", 5: "5 s", 7.5: "7.5 s", 120: "120 s"} {
		if got := formatMuteSeconds(v); got != want {
			t.Errorf("formatMuteSeconds(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestParseMuteSeconds(t *testing.T) {
	for in, want := range map[string]float64{
		"5": 5, " 7.5 ": 7.5, "7,5": 7.5, "8 s": 8, "8s": 8, "12 seconds": 12,
		"0.5": 0, "0": 0, // under a second: always
		"2.04": 2, "9999": maxMuteSeconds,
	} {
		if got, err := parseMuteSeconds(in); err != nil || got != want {
			t.Errorf("parseMuteSeconds(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-3", "s"} {
		if _, err := parseMuteSeconds(in); err == nil {
			t.Errorf("parseMuteSeconds(%q) took it", in)
		}
	}
}
