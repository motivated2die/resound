package audio

import (
	"math"
	"testing"
)

func db(g float32) float64 { return 20 * math.Log10(float64(g)) }

func TestGainCurve(t *testing.T) {
	for _, tc := range []struct {
		percent int
		wantDB  float64
	}{
		{100, 0}, {75, -10}, {50, -20}, {25, -30}, {10, -36}, {5, -42}, {200, 6.0}, {150, 3.5},
	} {
		if got := db(Gain(tc.percent)); math.Abs(got-tc.wantDB) > 0.1 {
			t.Errorf("Gain(%d) = %.1f dB, want %.1f dB", tc.percent, got, tc.wantDB)
		}
	}
	if Gain(0) != 0 || Gain(-5) != 0 {
		t.Error("0% (or less) must be silent")
	}
	// Every 5% step from 100% down to the fade changes the loudness by the
	// same 2 dB: as much at the top as at the bottom.
	for p := 100; p > 10; p -= 5 {
		if step := db(Gain(p)) - db(Gain(p-5)); math.Abs(step-2) > 0.01 {
			t.Errorf("the step %d→%d%% is %.2f dB, want 2 dB", p, p-5, step)
		}
	}
	// Quieter all the way down.
	for p := 100; p > 0; p-- {
		if Gain(p-1) >= Gain(p) {
			t.Fatalf("Gain(%d) >= Gain(%d)", p-1, p)
		}
	}
}

// Converting a linear-scale value keeps the loudness.
func TestFromLinear(t *testing.T) {
	for _, p := range []int{10, 25, 50, 80, 99} {
		got := FromLinear(p)
		if diff := db(Gain(got)) - db(float32(p)/100); math.Abs(diff) > 0.25 {
			t.Errorf("FromLinear(%d) = %d: %.2f dB louder than before", p, got, diff)
		}
	}
	for _, p := range []int{0, 100, 150, 200} {
		if FromLinear(p) != p {
			t.Errorf("FromLinear(%d) = %d, want unchanged", p, FromLinear(p))
		}
	}
	if FromLinear(50) != 85 {
		t.Errorf("FromLinear(50) = %d, want 85", FromLinear(50))
	}
}
