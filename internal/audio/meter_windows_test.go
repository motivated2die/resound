package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestSamplesPeak(t *testing.T) {
	f32 := make([]byte, 16)
	for i, v := range []float32{0.1, -0.6, 0.3, 0.2} {
		binary.LittleEndian.PutUint32(f32[4*i:], math.Float32bits(v))
	}
	i16 := make([]byte, 4)
	binary.LittleEndian.PutUint16(i16, uint16(0x4000))     // +0.5
	binary.LittleEndian.PutUint16(i16[2:], uint16(0xA000)) // -0.75
	i24 := []byte{0x00, 0x00, 0x20, 0x00, 0x00, 0xC0}      // +0.25, -0.5
	for _, c := range []struct {
		name string
		b    []byte
		f    sampleFormat
		want float32
	}{
		{"float32", f32, sampleFormat{Float: true, Bits: 32}, 0.6},
		{"int16", i16, sampleFormat{Bits: 16}, 0.75},
		{"int24", i24, sampleFormat{Bits: 24}, 0.5},
	} {
		if got := samplesPeak(c.b, c.f); math.Abs(float64(got-c.want)) > 1e-4 {
			t.Errorf("%s: peak = %v, want %v", c.name, got, c.want)
		}
	}
}
