package audio

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// EventKind identifies an engine event.
type EventKind int

const (
	// EventFirstBuffer fires when a voice's first samples were handed to the
	// device buffer. Delay is the time from Play() to that moment.
	EventFirstBuffer EventKind = iota
	// EventFinished fires when a voice played to its end or was stopped.
	EventFinished
	// EventOutputFailed fires when a device stream died (unplugged, format
	// change, exclusive-mode takeover). Err holds the reason.
	EventOutputFailed
)

// Event is emitted by outputs from their render threads. Consumers must not
// block; the channel is buffered and drops events when full.
type Event struct {
	Kind    EventKind
	Output  string // role name: "monitor" or "cable"
	VoiceID uint64
	Delay   time.Duration
	Err     error
}

// voice is one sound instance on one output. The same VoiceID is used on
// every output a sound plays on, so stop/pause address all copies at once.
type voice struct {
	id       uint64
	src      *Source
	pos      int // frames already rendered
	gain     float32
	absolute bool // gain replaces the master volume instead of scaling it
	paused   bool
	queuedAt time.Time
	started  bool
}

// Mixer sums all active voices into an interleaved stereo float32 buffer.
// Read runs on the render thread; all other methods may be called from any
// goroutine.
type Mixer struct {
	role   string
	events chan<- Event

	volume atomic.Uint32 // math.Float32bits of the master gain

	// Level meters: the voices before the master volume (the sounds
	// themselves) and the final mix sent to the device.
	srcPeak, outPeak peakHold

	mu     sync.Mutex
	voices []*voice
}

// NewMixer creates a mixer whose events are tagged with role.
func NewMixer(role string, events chan<- Event) *Mixer {
	m := &Mixer{role: role, events: events}
	m.SetVolume(1)
	return m
}

// SetVolume sets the master gain (0..1, values above 1 amplify).
func (m *Mixer) SetVolume(v float32) { m.volume.Store(math.Float32bits(v)) }

// Volume returns the master gain.
func (m *Mixer) Volume() float32 { return math.Float32frombits(m.volume.Load()) }

// Add queues a new voice. With absolute, gain replaces the master volume
// (per-sound custom volume); otherwise it scales it. The mixer owns src from
// now on and closes it when the voice ends.
func (m *Mixer) Add(id uint64, src *Source, gain float32, absolute bool, queuedAt time.Time) {
	m.mu.Lock()
	m.voices = append(m.voices, &voice{id: id, src: src, gain: gain, absolute: absolute, queuedAt: queuedAt})
	m.mu.Unlock()
}

// VoiceState is a snapshot of one voice, for progress displays.
type VoiceState struct {
	ID       uint64
	Position time.Duration
	Length   time.Duration
	Paused   bool
}

// Snapshot returns the state of all queued voices.
func (m *Mixer) Snapshot() []VoiceState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]VoiceState, 0, len(m.voices))
	for _, v := range m.voices {
		rate := time.Duration(v.src.Rate)
		out = append(out, VoiceState{
			ID:       v.id,
			Position: time.Duration(v.pos) * time.Second / rate,
			Length:   v.src.Length(),
			Paused:   v.paused,
		})
	}
	return out
}

// Stop removes the voice with the given ID. It reports whether it existed.
func (m *Mixer) Stop(id uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, v := range m.voices {
		if v.id == id {
			v.src.Close()
			m.voices = append(m.voices[:i], m.voices[i+1:]...)
			m.emit(Event{Kind: EventFinished, Output: m.role, VoiceID: id})
			return true
		}
	}
	return false
}

// StopAll removes every voice.
func (m *Mixer) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, v := range m.voices {
		v.src.Close()
		m.emit(Event{Kind: EventFinished, Output: m.role, VoiceID: v.id})
		m.voices[i] = nil
	}
	m.voices = m.voices[:0]
}

// Drain removes every voice without events. Outputs call it when their
// stream ends, so producers stop and files are closed.
func (m *Mixer) Drain() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, v := range m.voices {
		v.src.Close()
		m.voices[i] = nil
	}
	m.voices = m.voices[:0]
}

// SetPaused pauses or resumes a voice.
func (m *Mixer) SetPaused(id uint64, paused bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.voices {
		if v.id == id {
			v.paused = paused
		}
	}
}

// SetGain changes a playing voice's gain, e.g. when its volume is adjusted
// while it plays. With absolute, gain replaces the master volume.
func (m *Mixer) SetGain(id uint64, gain float32, absolute bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.voices {
		if v.id == id {
			v.gain, v.absolute = gain, absolute
		}
	}
}

// Active returns the number of voices currently queued.
func (m *Mixer) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.voices)
}

// Read fills out (interleaved stereo, len = 2*frames) with the mix of all
// voices. It never blocks for long: the lock only guards the voice list.
func (m *Mixer) Read(out []float32) {
	for i := range out {
		out[i] = 0
	}
	master := m.Volume()
	now := time.Now()
	var srcPeak float32

	m.mu.Lock()
	kept := m.voices[:0]
	for _, v := range m.voices {
		if v.paused {
			kept = append(kept, v)
			continue
		}
		g := v.gain * master
		if v.absolute {
			g = v.gain
		}
		// Play what the producer has buffered. If it falls behind, this
		// voice is silent for a moment (an underrun), never blocking.
		n := v.src.mixInto(out, g)
		v.pos += n
		if p := v.src.takePeak(); v.absolute {
			srcPeak = max(srcPeak, p) // a custom volume replaces the master: show the sound as it is
		} else {
			srcPeak = max(srcPeak, p*v.gain)
		}
		if !v.started && n > 0 {
			v.started = true
			m.emit(Event{Kind: EventFirstBuffer, Output: m.role, VoiceID: v.id, Delay: now.Sub(v.queuedAt)})
		}
		if v.src.finished() {
			v.src.Close()
			m.emit(Event{Kind: EventFinished, Output: m.role, VoiceID: v.id})
			continue
		}
		kept = append(kept, v)
	}
	// Clear the tail so finished voices can be garbage collected.
	for i := len(kept); i < len(m.voices); i++ {
		m.voices[i] = nil
	}
	m.voices = kept
	m.mu.Unlock()

	// Hard clip. A limiter can replace this later.
	var outPeak float32
	for i, s := range out {
		if s > 1 {
			out[i] = 1
		} else if s < -1 {
			out[i] = -1
		}
		outPeak = max(outPeak, abs32(out[i]))
	}
	m.srcPeak.raise(srcPeak)
	m.outPeak.raise(outPeak)
}

// takePeaks returns the loudest voice before the master volume and the
// loudest sample of the mix since the last call, and resets both.
func (m *Mixer) takePeaks() (src, out float32) { return m.srcPeak.take(), m.outPeak.take() }

func (m *Mixer) emit(e Event) {
	if m.events == nil {
		return
	}
	select {
	case m.events <- e:
	default: // never block the render thread
	}
}
