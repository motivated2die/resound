package audio

import "math"

// Volume percentages (the panel's sliders, per-sound volumes from the phone)
// follow a loudness curve rather than a straight line, because hearing is
// logarithmic: with a linear gain, 50% is only 6 dB quieter, so the upper
// half of a slider barely changes the loudness and the quiet range is
// crammed into its last few percent.
//
// The curve is linear in decibels: every percent changes the loudness by
// the same amount (2 dB per 5% step over a 40 dB range), so the slider
// feels even from top to bottom: 75% = −10 dB, 50% = −20 dB, 25% = −30 dB.
// Below 10% it fades linearly to true silence at 0%.
//
// Above 100% (per-sound boosts up to 200%), the gain stays linear as it
// always was, so a boost can't reach more than +6 dB and clip heavily.
const (
	volumeRangeDB = 40   // loudness between 100% and 0% before the fade
	volumeFadeAt  = 0.10 // below this share, fade linearly to silence
)

// Gain converts a volume percentage into a linear gain for the mixer.
func Gain(percent int) float32 {
	p := float64(max(0, percent)) / 100
	if p >= 1 {
		return float32(p)
	}
	at := func(p float64) float64 { return math.Pow(10, -volumeRangeDB*(1-p)/20) }
	if p < volumeFadeAt {
		return float32(at(volumeFadeAt) * p / volumeFadeAt)
	}
	return float32(at(p))
}

// Percent is the inverse of Gain: the volume percentage (0..200) whose gain
// is closest to g on the curve.
func Percent(g float32) int {
	switch {
	case g <= 0:
		return 0
	case g >= 1:
		return min(200, int(math.Round(float64(g)*100)))
	}
	knee := math.Pow(10, -volumeRangeDB*(1-volumeFadeAt)/20) // Gain at volumeFadeAt
	p := 1 + 20*math.Log10(float64(g))/volumeRangeDB
	if float64(g) < knee {
		p = volumeFadeAt * float64(g) / knee
	}
	return int(math.Round(p * 100))
}

// FromLinear converts a percentage saved before the curve (gain =
// percent/100) into the percentage with the closest loudness on the
// curve, e.g. 50 → 85. Values from 100 up are unchanged.
func FromLinear(percent int) int {
	if percent >= 100 || percent <= 0 {
		return percent
	}
	want := float64(percent) / 100
	best, bestDiff := 0, math.Inf(1)
	for p := 1; p <= 100; p++ {
		if d := math.Abs(math.Log(float64(Gain(p)) / want)); d < bestDiff {
			best, bestDiff = p, d
		}
	}
	return best
}
