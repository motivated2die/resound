package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMixerSumsAndFinishes(t *testing.T) {
	events := make(chan Event, 16)
	m := NewMixer("test", events)
	m.SetVolume(0.5)
	pcm := []float32{0.4, 0.4, 0.4, 0.4, 0.4, 0.4} // 3 frames
	m.Add(1, NewStaticSource("t", 48000, pcm), 1, false, time.Now())
	m.Add(2, NewStaticSource("t", 48000, pcm), 1, false, time.Now())

	out := make([]float32, 4) // 2 frames
	m.Read(out)
	for i, s := range out {
		if s < 0.399 || s > 0.401 { // 2 voices * 0.4 * 0.5
			t.Fatalf("sample %d = %v, want 0.4", i, s)
		}
	}
	m.Read(out) // frame 3, then silence; both voices finish
	if out[2] != 0 || out[3] != 0 {
		t.Fatalf("expected silence after clip end, got %v", out)
	}
	if m.Active() != 0 {
		t.Fatalf("voices still active: %d", m.Active())
	}
	var first, finished int
	for len(events) > 0 {
		switch (<-events).Kind {
		case EventFirstBuffer:
			first++
		case EventFinished:
			finished++
		}
	}
	if first != 2 || finished != 2 {
		t.Fatalf("events: first=%d finished=%d, want 2/2", first, finished)
	}
}

func TestMixerClips(t *testing.T) {
	m := NewMixer("test", nil)
	m.Add(1, NewStaticSource("t", 48000, []float32{0.9, -0.9}), 1, false, time.Now())
	m.Add(2, NewStaticSource("t", 48000, []float32{0.9, -0.9}), 1, false, time.Now())
	out := make([]float32, 2)
	m.Read(out)
	if out[0] != 1 || out[1] != -1 {
		t.Fatalf("expected hard clip to ±1, got %v", out)
	}
}

func TestChimeSource(t *testing.T) {
	const rate = 48000
	c := ChimeSource(rate)
	pcm := c.static
	if d := c.Length(); d < 2900*time.Millisecond || d > 3100*time.Millisecond {
		t.Fatalf("length = %v, want about 3 s", d)
	}
	var peak, left, right float64
	for i, v := range pcm {
		a := math.Abs(float64(v))
		peak = max(peak, a)
		if i < 0.15*rate*2 { // the first note leans left
			if i%2 == 0 {
				left += a
			} else {
				right += a
			}
		}
	}
	if peak < 0.29 || peak > 0.31 {
		t.Fatalf("peak = %.3f, want 0.30", peak)
	}
	if left < 1.2*right {
		t.Fatalf("first note should lean left (L %.0f, R %.0f)", left, right)
	}
	for _, v := range []float32{pcm[0], pcm[1], pcm[len(pcm)-2], pcm[len(pcm)-1]} {
		if math.Abs(float64(v)) > 0.01 {
			t.Fatalf("edges must be silent to avoid clicks, got %v", v)
		}
	}
	// It rings out: still audible two seconds in, quieter than the phrase.
	rms := func(from, to float64) float64 {
		var sum float64
		a, b := int(from*rate)*2, int(to*rate)*2
		for _, v := range pcm[a:b] {
			sum += float64(v) * float64(v)
		}
		return math.Sqrt(sum / float64(b-a))
	}
	phrase, tail := rms(0, 1.3), rms(2.0, 2.5)
	if tail < 0.005 || tail > phrase {
		t.Fatalf("rms: phrase %.4f, tail %.4f; want an audible, quieter tail", phrase, tail)
	}
	// Mellow: little energy in the sample-to-sample differences (high
	// frequencies) compared with the signal.
	var sig, diff float64
	for i := 2; i < len(pcm); i += 2 {
		sig += float64(pcm[i]) * float64(pcm[i])
		d := float64(pcm[i] - pcm[i-2])
		diff += d * d
	}
	if r := diff / sig; r > 0.02 {
		t.Fatalf("high-frequency ratio %.4f, want a muted sound (< 0.02)", r)
	}
	writeWAV(t, "test-sound", pcm, rate)
}

// writeWAV saves stereo float samples as a 16-bit WAV to
// $RESOUND_RENDER_DIR/name.wav when that is set, to listen to them.
func writeWAV(t *testing.T, name string, pcm []float32, rate int) {
	dir := os.Getenv("RESOUND_RENDER_DIR")
	if dir == "" {
		return
	}
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	data := uint32(len(pcm) * 2)
	b.WriteString("RIFF")
	le(36 + data)
	b.WriteString("WAVEfmt ")
	le(uint32(16))
	le(uint16(1)) // PCM
	le(uint16(2))
	le(uint32(rate))
	le(uint32(rate * 4))
	le(uint16(4))
	le(uint16(16))
	b.WriteString("data")
	le(data)
	for _, v := range pcm {
		le(int16(max(-1, min(1, v)) * 32767))
	}
	if err := os.WriteFile(filepath.Join(dir, name+".wav"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestToneSourceLength(t *testing.T) {
	c := ToneSource(440, 500*time.Millisecond, 48000)
	if c.Frames() != 24000 {
		t.Fatalf("frames = %d, want 24000", c.Frames())
	}
}

func TestMixerAbsoluteGainIgnoresMaster(t *testing.T) {
	m := NewMixer("test", nil)
	m.SetVolume(0.1)
	m.Add(1, NewStaticSource("t", 48000, []float32{0.5, 0.5}), 1.5, true, time.Now())
	out := make([]float32, 2)
	m.Read(out)
	if out[0] < 0.749 || out[0] > 0.751 {
		t.Fatalf("absolute gain sample = %v, want 0.75", out[0])
	}
}

func TestMixerWaitsForSlowProducer(t *testing.T) {
	events := make(chan Event, 16)
	m := NewMixer("test", events)
	feed := make(chan []float32)
	src := newStreams("slow", 48000, 1, 0, func(dst []float32) (int, error) {
		chunk, ok := <-feed
		if !ok {
			return 0, io.EOF
		}
		return copy(dst, chunk), nil
	}, func() {}, nil)[0]
	m.Add(1, src, 1, false, time.Now())

	out := make([]float32, 4)
	m.Read(out) // nothing produced yet: silence, voice stays
	if m.Active() != 1 || out[0] != 0 {
		t.Fatalf("voice dropped or not silent: active=%d out=%v", m.Active(), out)
	}
	feed <- []float32{0.5, 0.5}
	close(feed)
	<-src.ready
	m.Read(out)
	if out[0] != 0.5 || out[2] != 0 {
		t.Fatalf("out = %v, want the one produced frame", out)
	}
	if m.Active() != 0 {
		t.Fatal("voice should finish once the source is done and drained")
	}
}

func TestMixerStopClosesSources(t *testing.T) {
	m := NewMixer("test", nil)
	closed := make(chan struct{})
	src := newStreams("endless", 48000, 1, 0, func(dst []float32) (int, error) {
		for i := range dst {
			dst[i] = 0.1
		}
		return len(dst), nil
	}, func() { close(closed) }, nil)[0]
	if err := src.waitReady(); err != nil {
		t.Fatal(err)
	}
	m.Add(1, src, 1, false, time.Now())
	m.Read(make([]float32, 256))
	m.StopAll()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("producer still running after StopAll")
	}
}

func TestMixerSetGainWhilePlaying(t *testing.T) {
	m := NewMixer("monitor", nil)
	m.SetVolume(0.5)
	m.Add(1, NewStaticSource("t", 48000, []float32{0.4, 0.4, 0.4, 0.4}), 1, false, time.Now())
	out := make([]float32, 2)
	m.Read(out)
	if out[0] != 0.2 { // follows the master volume
		t.Fatalf("before: %v", out[0])
	}
	m.SetGain(1, 2, true) // custom volume: replaces the master
	m.Read(out)
	if out[0] != 0.8 {
		t.Fatalf("after: %v", out[0])
	}
}
