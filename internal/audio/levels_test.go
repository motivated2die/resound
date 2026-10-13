package audio

import (
	"math"
	"testing"
	"time"
)

func TestPeakHold(t *testing.T) {
	var p peakHold
	p.raise(0.25)
	p.raise(0.5)
	p.raise(0.1) // lower values don't replace the peak
	if got := p.take(); got != 0.5 {
		t.Fatalf("take = %v, want 0.5", got)
	}
	if got := p.take(); got != 0 {
		t.Fatalf("second take = %v, want 0 (reset)", got)
	}
}

func TestMeterPosition(t *testing.T) {
	for _, c := range []struct {
		peak float32
		want float64
	}{
		{0, 0},
		{-1, 0},
		{1, 1},
		{2, 1}, // clipped input stays full
		{float32(math.Pow(10, MeterFloorDB/20.0)), 0},
		{float32(math.Pow(10, MeterFloorDB/40.0)), 0.5}, // half the dB range
	} {
		if got := MeterPosition(c.peak); math.Abs(got-c.want) > 1e-4 {
			t.Errorf("MeterPosition(%v) = %v, want %v", c.peak, got, c.want)
		}
	}
}

func TestMixerPeaks(t *testing.T) {
	m := NewMixer("test", nil)
	m.SetVolume(0.5)
	m.Add(1, NewStaticSource("a", 48000, []float32{0.4, -0.8, 0.2, 0.2}), 1, false, time.Now())
	m.Add(2, NewStaticSource("b", 48000, []float32{0.3, 0.3, 0.3, 0.3}), 0.5, true, time.Now()) // custom volume
	out := make([]float32, 4)
	m.Read(out)
	src, mix := m.takePeaks()
	// Sounds: the loudest voice before the master volume (|-0.8| at gain 1;
	// the custom-volume voice counts as it is, 0.3).
	if math.Abs(float64(src)-0.8) > 1e-6 {
		t.Errorf("source peak = %v, want 0.8", src)
	}
	// Mix: frame 1 is 0.4*0.5+0.3*0.5 = 0.35 left, -0.8*0.5+0.3*0.5 = -0.25 right.
	if math.Abs(float64(mix)-0.35) > 1e-6 {
		t.Errorf("mix peak = %v, want 0.35", mix)
	}
	if src, mix := m.takePeaks(); src != 0 || mix != 0 {
		t.Errorf("peaks after take = %v, %v; want 0, 0", src, mix)
	}
	m.Read(out) // the voices ended: silence
	if src, mix := m.takePeaks(); src != 0 || mix != 0 {
		t.Errorf("peaks of silence = %v, %v; want 0, 0", src, mix)
	}
}

func TestPausedVoiceHasNoLevel(t *testing.T) {
	m := NewMixer("test", nil)
	m.Add(1, NewStaticSource("a", 48000, []float32{0.5, 0.5, 0.5, 0.5}), 1, false, time.Now())
	m.SetPaused(1, true)
	m.Read(make([]float32, 4))
	if src, mix := m.takePeaks(); src != 0 || mix != 0 {
		t.Fatalf("paused voice peaks = %v, %v; want 0, 0", src, mix)
	}
}
