package audio

import (
	"math"
	"sync/atomic"
)

// Level meters. Render and capture threads raise a peakHold with the
// loudest sample they handled; the panel takes it (and resets it) about 30
// times a second, so no peak between two looks is missed.

// peakHold is the highest sample magnitude since it was last taken.
type peakHold struct{ bits atomic.Uint32 }

// raise lifts the held peak to v if v is higher. Lock-free; any thread.
func (p *peakHold) raise(v float32) {
	for {
		old := p.bits.Load()
		if v <= math.Float32frombits(old) || p.bits.CompareAndSwap(old, math.Float32bits(v)) {
			return
		}
	}
}

// take returns the held peak and resets it.
func (p *peakHold) take() float32 { return math.Float32frombits(p.bits.Swap(0)) }

// Levels are the peaks (0..1, linear) since the last Engine.Levels call.
type Levels struct {
	Sounds  float32 // the playing sounds themselves, before the output volumes
	Monitor float32 // what Resound sends to "You hear", after its volume
	Cable   float32 // what Resound sends to "Others hear", after its volume
}

// MeterFloorDB is the quietest level a meter shows; anything below is empty.
const MeterFloorDB = -54

// MeterPosition maps a linear peak to a meter fill (0..1) on a decibel
// scale from MeterFloorDB to 0 dB, which spreads speech and quiet sounds
// over the bar instead of crowding them at its start.
func MeterPosition(peak float32) float64 {
	if peak <= 0 {
		return 0
	}
	db := 20 * math.Log10(float64(peak))
	return max(0, min(1, (db-MeterFloorDB)/-MeterFloorDB))
}
