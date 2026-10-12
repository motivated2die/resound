package audio

import (
	"io"
	"math"
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
	c := ChimeSource(48000)
	pcm := c.static
	if d := c.Length(); d < 400*time.Millisecond || d > 500*time.Millisecond {
		t.Fatalf("length = %v, want about 0.45 s", d)
	}
	var peak, left, right float64
	for i, v := range pcm {
		a := math.Abs(float64(v))
		peak = max(peak, a)
		if i < 0.07*48000*2 { // first note only
			if i%2 == 0 {
				left += a
			} else {
				right += a
			}
		}
	}
	if peak < 0.3 || peak > 0.33 {
		t.Fatalf("peak = %.3f, want 0.32", peak)
	}
	if left < 1.5*right {
		t.Fatalf("first note should lean left (L %.0f, R %.0f)", left, right)
	}
	for _, v := range []float32{pcm[0], pcm[1], pcm[len(pcm)-2], pcm[len(pcm)-1]} {
		if math.Abs(float64(v)) > 0.01 {
			t.Fatalf("edges must be silent to avoid clicks, got %v", v)
		}
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
