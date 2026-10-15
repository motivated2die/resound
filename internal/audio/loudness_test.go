package audio

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
)

// tone streams a stereo sine of amp (peak) at freq Hz for d.
func tone(rate int, freq, amp float64, d time.Duration) beep.Streamer {
	n := int(d.Seconds() * float64(rate))
	i := 0
	return beep.StreamerFunc(func(buf [][2]float64) (int, bool) {
		if i >= n {
			return 0, false
		}
		k := 0
		for ; k < len(buf) && i < n; k, i = k+1, i+1 {
			v := amp * math.Sin(2*math.Pi*freq*float64(i)/float64(rate))
			buf[k] = [2]float64{v, v}
		}
		return k, true
	})
}

// BS.1770-4's calibration: a 0 dBFS 997 Hz sine on one channel reads
// −3.01 LUFS, so on both channels −0.01 LUFS. The K-weighting must give it
// at every common sample rate.
func TestLoudnessCalibration(t *testing.T) {
	for _, rate := range []int{22050, 44100, 48000, 96000} {
		l, err := measure(tone(rate, 997, 1, 2*time.Second), rate)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(l.LUFS-(-0.01)) > 0.05 {
			t.Errorf("%d Hz: %.3f LUFS, want -0.01", rate, l.LUFS)
		}
		if !l.Complete || l.Length != 2*time.Second {
			t.Errorf("%d Hz: length %v complete %v", rate, l.Length, l.Complete)
		}
	}
}

// At 48 kHz the derived filter must be the standard's.
func TestKWeighting48k(t *testing.T) {
	shelf, hp := kWeighting(48000)
	want := []float64{1.53512485958697, -2.69169618940638, 1.19839281085285, -1.69065929318241, 0.73248077421585}
	got := []float64{shelf.b0, shelf.b1, shelf.b2, shelf.a1, shelf.a2}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Errorf("shelf[%d] = %.14f, want %.14f", i, got[i], want[i])
		}
	}
	if math.Abs(hp.a1-(-1.99004745483398)) > 1e-9 || math.Abs(hp.a2-0.99007225036621) > 1e-9 {
		t.Errorf("high pass a = %.14f %.14f", hp.a1, hp.a2)
	}
}

// Gating: silence around a sound doesn't make it quieter (only the blocks
// overlapping its edges count, as the standard has it), and a sound shorter
// than one block is still measured.
func TestLoudnessGating(t *testing.T) {
	const rate = 48000
	ref, _ := measure(tone(rate, 997, 0.5, 2*time.Second), rate)
	padded, _ := measure(beep.Seq(beep.Silence(rate*3), tone(rate, 997, 0.5, 2*time.Second), beep.Silence(rate*3)), rate)
	if math.Abs(ref.LUFS-padded.LUFS) > 1 {
		t.Errorf("silence moved the loudness: %.2f → %.2f LUFS", ref.LUFS, padded.LUFS)
	}
	short, err := measure(tone(rate, 997, 0.5, 200*time.Millisecond), rate)
	if err != nil || math.Abs(short.LUFS-ref.LUFS) > 0.3 {
		t.Errorf("200 ms: %.2f LUFS (%v), want ≈ %.2f", short.LUFS, err, ref.LUFS)
	}
	silent, _ := measure(beep.Silence(rate), rate)
	if silent.LUFS != SilentLUFS || NormGain(silent.LUFS, silent.Peak) != 1 {
		t.Errorf("silence: %.2f LUFS, gain %v", silent.LUFS, NormGain(silent.LUFS, silent.Peak))
	}
}

// A single click doesn't set the robust peak, so it can't hold back the
// boost of an otherwise quiet sound.
func TestRobustPeak(t *testing.T) {
	const rate = 48000
	quiet := tone(rate, 440, 0.02, 3*time.Second)    // −34 dBFS peak
	click := tone(rate, 2000, 1, 2*time.Millisecond) // 0 dBFS, 2 ms
	l, _ := measure(beep.Seq(quiet, click), rate)
	if l.Peak > -33 || l.Peak < -35.5 {
		t.Errorf("robust peak %.2f dBFS, want ≈ −34 (the click ignored)", l.Peak)
	}
	if g := 20 * math.Log10(float64(NormGain(l.LUFS, l.Peak))); math.Abs(g-maxBoostDB) > 0.01 {
		t.Errorf("boost %.2f dB, want the maximum %v", g, maxBoostDB)
	}
}

func TestNormGain(t *testing.T) {
	db := func(lufs, peak float64) float64 { return 20 * math.Log10(float64(NormGain(lufs, peak))) }
	for _, c := range []struct{ lufs, peak, want float64 }{
		{-10, 0, -6},            // loud: turned down
		{LoudnessTarget, -1, 0}, // on target
		{-20, -10, 4},           // quiet: boosted
		{-40, -10, 10},          // boost stops where the robust peak reaches full scale
		{-40, -30, maxBoostDB},  // and at the maximum boost
		{-10, 3, -6},            // a peak never limits a cut
		{20, 0, minGainDB},      // nonsense: bounded
		{SilentLUFS, -100, 0},   // silence: unchanged
	} {
		if got := db(c.lufs, c.peak); math.Abs(got-c.want) > 0.01 {
			t.Errorf("NormGain(%v, %v) = %.2f dB, want %v", c.lufs, c.peak, got, c.want)
		}
	}
}

func TestMeasureLoudnessFile(t *testing.T) {
	l, err := MeasureLoudness(writeTestWAV(t, t.TempDir(), time.Second))
	if err != nil || l.Length != time.Second || !l.Complete {
		t.Fatalf("got %+v, %v", l, err)
	}
	// Long files are measured in segments, with the same result for a
	// steady tone, and their length from the decoder.
	long, err := MeasureLoudness(writeTestWAV(t, t.TempDir(), 100*time.Second))
	if err != nil || long.Complete || long.Length != 100*time.Second || math.Abs(long.LUFS-l.LUFS) > 0.1 {
		t.Errorf("100 s: got %+v, %v; 1 s: %.2f LUFS", long, err, l.LUFS)
	}
	// MP3 files go through go-mp3 directly.
	if l, err := MeasureLoudness(writeTestMP3(t, t.TempDir(), 100, false, 0)); err != nil || l.LUFS != SilentLUFS || !l.Complete || l.Length < 2*time.Second {
		t.Errorf("silent MP3: got %+v, %v", l, err)
	}
	if _, err := MeasureLoudness(filepath.Join(t.TempDir(), "missing.mp3")); err == nil {
		t.Error("missing file measured")
	}
}

// With RESOUND_LOUDNESS_DIR set, measures every sound in that folder and
// reports loudness, peaks, gain and timing: go test ./internal/audio -run
// LoudnessReport -v.
func TestLoudnessReport(t *testing.T) {
	dir := os.Getenv("RESOUND_LOUDNESS_DIR")
	if dir == "" {
		t.Skip("RESOUND_LOUDNESS_DIR not set")
	}
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && slices.Contains(SupportedExtensions, strings.ToLower(filepath.Ext(p))) {
			files = append(files, p)
		}
		return nil
	})
	var total, audio time.Duration
	for _, f := range files {
		start := time.Now()
		l, err := MeasureLoudness(f)
		took := time.Since(start)
		total += took
		if err != nil {
			t.Logf("%-50.50s error: %v", filepath.Base(f), err)
			continue
		}
		audio += l.Length
		t.Logf("%-50.50s %7.2f LUFS  peak %6.2f  gain %+6.2f dB  %7.1fs  %6v",
			filepath.Base(f), l.LUFS, l.Peak, 20*math.Log10(float64(NormGain(l.LUFS, l.Peak))),
			l.Length.Seconds(), took.Round(time.Millisecond))
	}
	t.Logf("%d files, %v of audio measured in %v (%.0f× real time)",
		len(files), audio.Round(time.Second), total.Round(time.Millisecond), audio.Seconds()/total.Seconds())
}

// The decoder applies the normalization gain to every sample.
func TestDecoderGain(t *testing.T) {
	path := writeTestWAV(t, t.TempDir(), 100*time.Millisecond)
	read := func(gain float32) []float32 {
		fill, _, closer, err := openDecoder(path, 44100, gain)
		if err != nil {
			t.Fatal(err)
		}
		defer closer()
		buf := make([]float32, 2048)
		n, _ := fill(buf)
		return buf[:n]
	}
	plain, half := read(1), read(0.5)
	if len(plain) == 0 || len(plain) != len(half) {
		t.Fatalf("read %d and %d samples", len(plain), len(half))
	}
	for i := range plain {
		if half[i] != plain[i]*0.5 {
			t.Fatalf("sample %d: %v at gain 0.5, %v at 1", i, half[i], plain[i])
		}
	}
}
