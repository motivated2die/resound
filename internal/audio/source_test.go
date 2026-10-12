package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/wav"
)

// writeTestWAV writes d of a 440 Hz tone (amplitude 0.3) at 44.1 kHz.
func writeTestWAV(t testing.TB, dir string, d time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, "tone.wav")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tone := ToneSource(440, d, 44100)
	pos := 0
	src := beep.StreamerFunc(func(samples [][2]float64) (int, bool) {
		n := 0
		for n < len(samples) && pos < tone.Frames() {
			samples[n] = [2]float64{float64(tone.static[2*pos]), float64(tone.static[2*pos+1])}
			n++
			pos++
		}
		return n, n > 0
	})
	if err := wav.Encode(f, src, beep.Format{SampleRate: 44100, NumChannels: 2, Precision: 2}); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeTestMP3 writes frames of silent MPEG-1 layer III audio (128 kbit/s,
// 44.1 kHz, 417-byte frames). With info, the first frame carries an Info
// header with the frame count; id3 prepends an ID3v2 tag of that many bytes.
func writeTestMP3(t testing.TB, dir string, frames int, info bool, id3 int) string {
	t.Helper()
	var b bytes.Buffer
	if id3 > 0 {
		b.Write([]byte{'I', 'D', '3', 3, 0, 0, byte(id3 >> 21 & 0x7F), byte(id3 >> 14 & 0x7F), byte(id3 >> 7 & 0x7F), byte(id3 & 0x7F)})
		b.Write(make([]byte, id3))
	}
	for i := 0; i < frames; i++ {
		frame := make([]byte, 417)
		copy(frame, []byte{0xFF, 0xFB, 0x90, 0x00})
		if i == 0 && info {
			copy(frame[36:], "Info")
			binary.BigEndian.PutUint32(frame[40:], 1) // frames field present
			binary.BigEndian.PutUint32(frame[44:], uint32(frames-1))
		}
		b.Write(frame)
	}
	path := filepath.Join(dir, "silence.mp3")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// drain consumes a source like a mixer would and returns its samples.
func drain(t testing.TB, s *Source, keep bool) (frames int, peak float32, pcm []float32) {
	t.Helper()
	if err := s.waitReady(); err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, 960*2) // one 20 ms period at 48 kHz
	deadline := time.Now().Add(30 * time.Second)
	for !s.finished() {
		if time.Now().After(deadline) {
			t.Fatal("source never finished")
		}
		clear(buf)
		n := s.mixInto(buf, 1)
		if n == 0 {
			time.Sleep(100 * time.Microsecond)
			continue
		}
		frames += n
		for _, v := range buf[:2*n] {
			peak = max(peak, v)
		}
		if keep {
			pcm = append(pcm, buf[:2*n]...)
		}
	}
	return frames, peak, pcm
}

func TestFileStreamResamples(t *testing.T) {
	path := writeTestWAV(t, t.TempDir(), time.Second)
	var exact atomic.Int64
	srcs, err := openFileStreams(path, 48000, 1, 0, func(f int64) { exact.Store(f) })
	if err != nil {
		t.Fatal(err)
	}
	s := srcs[0]
	defer s.Close()
	frames, peak, _ := drain(t, s, false)
	if frames < 47900 || frames > 48100 {
		t.Fatalf("resampled frames = %d, want ~48000", frames)
	}
	if peak < 0.25 || peak > 0.35 {
		t.Fatalf("peak = %v, want ~0.3", peak)
	}
	if s.Frames() != frames || exact.Load() != int64(frames) {
		t.Fatalf("length = %d, reported %d, played %d", s.Frames(), exact.Load(), frames)
	}
}

// Two outputs at the same rate share one decoder: both get every sample,
// the file is decoded once, and stopping one output does not stop the other.
func TestFanOutDecodesOnce(t *testing.T) {
	const total = 48000 * 3 // 3 s of frames
	var decoded atomic.Int64
	closed := make(chan struct{})
	fill := func(dst []float32) (int, error) {
		left := total - int(decoded.Load())
		n := min(len(dst)/2, left)
		for i := 0; i < n; i++ {
			v := float32(int(decoded.Load())+i) / total
			dst[2*i], dst[2*i+1] = v, v
		}
		decoded.Add(int64(n))
		if decoded.Load() == total {
			return 2 * n, io.EOF
		}
		return 2 * n, nil
	}
	srcs := newStreams("t", 48000, 2, total, fill, func() { close(closed) }, nil)
	a, b := srcs[0], srcs[1]
	if err := a.waitReady(); err != nil {
		t.Fatal(err)
	}
	// Play both like two outputs would, in alternation.
	buf := make([]float32, 960*2)
	var gotA, gotB []float32
	for !a.finished() || !b.finished() {
		for _, x := range []struct {
			s   *Source
			out *[]float32
		}{{a, &gotA}, {b, &gotB}} {
			clear(buf)
			n := x.s.mixInto(buf, 1)
			*x.out = append(*x.out, buf[:2*n]...)
		}
		time.Sleep(50 * time.Microsecond)
	}
	if decoded.Load() != total || len(gotA) != 2*total || !equal(gotA, gotB) {
		t.Fatalf("decoded %d frames; outputs got %d and %d samples", decoded.Load(), len(gotA)/2, len(gotB)/2)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("file not closed after the end")
	}

	// Stopping one output: the other plays on; stopping both ends decoding.
	start := func() ([]*Source, chan struct{}) {
		decoded.Store(0)
		done := make(chan struct{})
		return newStreams("t", 48000, 2, total, fill, func() { close(done) }, nil), done
	}
	srcs, _ = start()
	srcs[0].waitReady()
	srcs[0].Close()
	if frames, _, _ := drain(t, srcs[1], false); frames != total {
		t.Fatalf("remaining output played %d of %d frames", frames, total)
	}

	srcs, stopped := start()
	srcs[0].waitReady()
	srcs[0].Close()
	srcs[1].Close()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("decoder still running after both outputs stopped")
	}
	if decoded.Load() >= total {
		t.Fatal("decoded to the end although both outputs stopped")
	}
}

func equal(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Long files must start as fast as short ones: nothing is read or decoded
// beyond the first buffer before playback starts.
func TestStartLatencyIndependentOfLength(t *testing.T) {
	if testing.Short() {
		t.Skip("writes long files")
	}
	dir := t.TempDir()
	cases := map[string]string{
		"10 min WAV": writeTestWAV(t, dir, 10*time.Minute),
		"60 min MP3": writeTestMP3(t, t.TempDir(), 60*60*44100/1152, false, 0),
	}
	for name, path := range cases {
		info, _ := os.Stat(path)
		var best time.Duration
		for i := range 3 { // best of three: the file is in the OS cache after the first
			start := time.Now()
			srcs, err := openFileStreams(path, 48000, 2, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range srcs {
				if err := s.waitReady(); err != nil {
					t.Fatal(err)
				}
			}
			d := time.Since(start)
			for _, s := range srcs {
				s.Close()
			}
			if i == 0 || d < best {
				best = d
			}
		}
		t.Logf("%s (%s): ready to play in %v", name, FormatBytes(info.Size()), best.Round(10*time.Microsecond))
		if best > 50*time.Millisecond {
			t.Errorf("%s: start took %v", name, best)
		}
	}
}

// A long sound is streamed with a fixed read-ahead: memory does not grow
// with its length.
func TestLongSoundUsesBoundedMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a two-minute file")
	}
	path := writeTestWAV(t, t.TempDir(), 2*time.Minute)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	srcs, err := openFileStreams(path, 48000, 2, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	var peakHeap uint64
	buf := make([]float32, 960*2)
	frames := 0
	srcs[0].waitReady()
	for i := 0; !srcs[0].finished(); i++ {
		n := 0
		for _, s := range srcs {
			clear(buf)
			n = s.mixInto(buf, 1)
		}
		frames += n
		if n == 0 {
			time.Sleep(100 * time.Microsecond)
		}
		if i%2000 == 0 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			peakHeap = max(peakHeap, m.HeapInuse)
		}
	}
	if frames < 120*48000-200 {
		t.Fatalf("frames = %d", frames)
	}
	growth := int64(peakHeap) - int64(before.HeapInuse)
	t.Logf("2 min sound on 2 outputs: heap growth %s (fully decoded it would be %s)", FormatBytes(growth), FormatBytes(2*120*48000*8))
	if growth > 8<<20 {
		t.Fatalf("heap grew by %s while streaming", FormatBytes(growth))
	}
}

func TestProbeDuration(t *testing.T) {
	near := func(got, want time.Duration, tol float64) bool {
		return float64(absDur(got-want)) <= tol*float64(want)
	}
	wavPath := writeTestWAV(t, t.TempDir(), 1500*time.Millisecond)
	if d, err := ProbeDuration(wavPath); err != nil || !near(d, 1500*time.Millisecond, 0.001) {
		t.Fatalf("wav: %v %v", d, err)
	}
	const frames = 2000 // 52.2 s
	want := time.Duration(frames-1) * 1152 * time.Second / 44100
	if d, err := ProbeDuration(writeTestMP3(t, t.TempDir(), frames, true, 0)); err != nil || d != want {
		t.Fatalf("mp3 with Info header: %v %v, want %v", d, err, want)
	}
	cbr := time.Duration(frames) * 1152 * time.Second / 44100
	if d, err := ProbeDuration(writeTestMP3(t, t.TempDir(), frames, false, 0)); err != nil || !near(d, cbr, 0.01) {
		t.Fatalf("CBR mp3: %v %v, want ~%v", d, err, cbr)
	}
	// A 300 KB ID3 tag (cover art) before the audio.
	if d, err := ProbeDuration(writeTestMP3(t, t.TempDir(), frames, false, 300<<10)); err != nil || !near(d, cbr, 0.01) {
		t.Fatalf("mp3 after a large ID3 tag: %v %v, want ~%v", d, err, cbr)
	}
	// The synthetic MP3 really decodes, and to the probed length.
	path := writeTestMP3(t, t.TempDir(), 200, false, 0)
	srcs, err := openFileStreams(path, 44100, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _, _ := drain(t, srcs[0], false)
	probed, _ := ProbeDuration(path)
	if played := time.Duration(got) * time.Second / 44100; !near(played, probed, 0.02) {
		t.Fatalf("decoded %v, probed %v", played, probed)
	}
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func TestRingWrapsAround(t *testing.T) {
	q := newRing(4 * chunkFrames)
	n := len(q.buf)
	in := make([]float32, n-6)
	for i := range in {
		in[i] = 1
	}
	q.write(in)
	out := make([]float32, n-6)
	if got := q.mixInto(out, 1); got != n-6 {
		t.Fatalf("read %d", got)
	}
	q.write([]float32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) // crosses the end
	out = make([]float32, 10)
	if got := q.mixInto(out, 2); got != 10 || out[0] != 2 || out[9] != 20 {
		t.Fatalf("wrapped read = %d %v", got, out)
	}
	if !q.empty() || q.free() != n {
		t.Fatal("ring not empty after reading everything")
	}
}
