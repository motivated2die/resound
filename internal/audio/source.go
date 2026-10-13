package audio

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// prebuffer is how much audio must be decoded before playback may start;
	// the rest is decoded while the sound plays. Decoding runs many times
	// faster than real time, so this costs a few milliseconds.
	prebuffer = 100 * time.Millisecond
	// ringDuration is how far a playing sound is decoded ahead. It bounds
	// the memory of a playing sound (~190 KB per output at 48 kHz) no matter
	// how long the file is, and covers disk or CPU hiccups of up to this length.
	ringDuration = 500 * time.Millisecond
	// chunkFrames is how many frames a producer decodes per step.
	chunkFrames = 2048
	// resampleQuality is beep's resampler quality (1-64). 4 is good and cheap.
	resampleQuality = 4
)

// fillFunc writes up to len(dst) samples (interleaved stereo, so an even
// count) and returns how many it wrote. It returns io.EOF with the last
// samples, or another error if reading failed.
type fillFunc func(dst []float32) (int, error)

// Source is one playing copy of a sound for one output: interleaved stereo
// float32 at Rate. Streaming sources are filled by a producer goroutine
// that decodes the file into a small ring buffer per output; the output's
// mixer drains its ring on the render thread without ever blocking or
// touching the disk. One producer (one decoder) serves every output that
// runs at the same sample rate. A static source holds its samples in memory
// (test tones).
type Source struct {
	Name string
	Rate int

	length   atomic.Int64 // expected total frames, exact once done
	produced atomic.Int64 // frames written so far

	static []float32 // whole sound (static sources only)
	spos   int       // samples consumed from static (render thread only)
	peak   float32   // loudest sample mixed from static since takePeak (render thread only)

	ring      *ring
	p         *producer
	closed    atomic.Bool
	done      atomic.Bool   // the producer wrote everything (or failed)
	err       atomic.Value  // error, stored before done
	ready     chan struct{} // closed once prebuffer is reached or the producer ended
	readyOnce sync.Once
}

// producer decodes one sound into the rings of one or more sources.
type producer struct {
	sources []*Source
	wake    chan struct{} // consumers read something, or a source closed
}

// NewStaticSource wraps decoded PCM held in memory.
func NewStaticSource(name string, rate int, pcm []float32) *Source {
	s := &Source{Name: name, Rate: rate, static: pcm, ready: make(chan struct{})}
	s.length.Store(int64(len(pcm) / 2))
	s.produced.Store(int64(len(pcm) / 2))
	s.done.Store(true)
	s.markReady()
	return s
}

// newStreams starts one producer that decodes from fill into n sources (one
// per output at this rate). expected is the best estimate of the length in
// frames (0 = unknown). closer runs when the producer ends. onEnd, if set,
// receives the exact length once the whole sound was decoded.
func newStreams(name string, rate, n int, expected int64, fill fillFunc, closer func(), onEnd func(frames int64)) []*Source {
	p := &producer{wake: make(chan struct{}, 1)}
	ringSamples := int(int64(rate) * int64(ringDuration) / int64(time.Second) * 2)
	for range n {
		s := &Source{Name: name, Rate: rate, ring: newRing(ringSamples), p: p, ready: make(chan struct{})}
		s.length.Store(expected)
		p.sources = append(p.sources, s)
	}
	go p.run(fill, closer, onEnd)
	return append([]*Source(nil), p.sources...)
}

func (p *producer) run(fill fillFunc, closer func(), onEnd func(int64)) {
	defer closer()
	tmp := make([]float32, chunkFrames*2)
	rate := p.sources[0].Rate
	readyAt := int64(prebuffer) * int64(rate) / int64(time.Second)
	var produced int64
	for {
		// The fill level is set by the slowest open output; closed ones
		// (stopped, or their output went away) no longer count.
		space, open := len(tmp), 0
		for _, s := range p.sources {
			if !s.closed.Load() {
				open++
				space = min(space, s.ring.free())
			}
		}
		if open == 0 {
			p.finish(produced, nil)
			return
		}
		if space < chunkFrames { // full: wait until the mixers consumed some
			<-p.wake
			continue
		}
		n, err := fill(tmp[:space&^1])
		n &^= 1
		if n > 0 {
			produced += int64(n / 2)
			for _, s := range p.sources {
				if !s.closed.Load() {
					s.ring.write(tmp[:n])
				}
				s.produced.Store(produced)
				if produced >= readyAt {
					s.markReady()
				}
			}
		}
		if err != nil || n == 0 {
			if err != nil && !errors.Is(err, io.EOF) {
				p.finish(produced, fmt.Errorf("decoding %s: %w", p.sources[0].Name, err))
				return
			}
			p.finish(produced, nil)
			if onEnd != nil && open == len(p.sources) {
				onEnd(produced) // decoded to the end: the exact length
			}
			return
		}
	}
}

func (p *producer) finish(frames int64, err error) {
	for _, s := range p.sources {
		if err != nil {
			s.err.Store(err)
		}
		s.length.Store(frames)
		s.done.Store(true)
		s.markReady()
	}
}

func (s *Source) markReady() { s.readyOnce.Do(func() { close(s.ready) }) }

// waitReady blocks until enough audio for a clean start is buffered.
func (s *Source) waitReady() error {
	<-s.ready
	if s.produced.Load() == 0 {
		if err := s.Err(); err != nil {
			return err
		}
		return fmt.Errorf("%s contains no audio", s.Name)
	}
	return nil
}

// Close detaches the source from its producer; the producer stops and
// closes the file when no source of it is left. Close never blocks, so the
// render thread may call it.
func (s *Source) Close() {
	if s.closed.Swap(true) || s.p == nil {
		return
	}
	s.p.signal()
}

func (p *producer) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Err returns the error that ended the producer early, if any.
func (s *Source) Err() error {
	err, _ := s.err.Load().(error)
	return err
}

// Frames returns the expected total length in frames (exact once done).
func (s *Source) Frames() int { return int(s.length.Load()) }

// Length returns the expected total length.
func (s *Source) Length() time.Duration {
	return time.Duration(s.length.Load()) * time.Second / time.Duration(s.Rate)
}

// mixInto adds up to len(out)/2 frames times gain into out and returns the
// number of frames consumed. Render thread only.
func (s *Source) mixInto(out []float32, gain float32) int {
	if s.ring == nil {
		src := s.static[min(s.spos, len(s.static)):]
		n := min(len(src), len(out)) &^ 1
		for i, v := range src[:n] {
			out[i] += v * gain
			s.peak = max(s.peak, abs32(v))
		}
		s.spos += n
		return n / 2
	}
	n := s.ring.mixInto(out, gain)
	if n > 0 {
		s.p.signal() // let the producer refill
	}
	return n / 2
}

// takePeak returns the loudest sample (before gain) mixed since the last
// call, for the level meters. Render thread only.
func (s *Source) takePeak() float32 {
	if s.ring == nil {
		p := s.peak
		s.peak = 0
		return p
	}
	p := s.ring.peak
	s.ring.peak = 0
	return p
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// finished reports whether everything was produced and consumed.
func (s *Source) finished() bool {
	if s.ring == nil {
		return s.spos >= len(s.static)
	}
	return s.done.Load() && s.ring.empty() // done first: then the ring is final
}

// ring is a single-producer single-consumer float32 ring buffer. The
// counters only grow; their difference is the fill level.
type ring struct {
	buf  []float32
	r, w atomic.Uint64
	peak float32 // loudest sample mixed since Source.takePeak (consumer only)
}

func newRing(samples int) *ring {
	samples = max(samples, 4*chunkFrames) &^ 1
	return &ring{buf: make([]float32, samples)}
}

func (q *ring) free() int   { return len(q.buf) - int(q.w.Load()-q.r.Load()) }
func (q *ring) empty() bool { return q.w.Load() == q.r.Load() }

// write appends src; the caller made sure it fits. Producer only.
func (q *ring) write(src []float32) {
	w := q.w.Load()
	i := int(w % uint64(len(q.buf)))
	c := copy(q.buf[i:], src)
	copy(q.buf, src[c:])
	q.w.Store(w + uint64(len(src))) // publish after the samples
}

// mixInto adds what is available (up to len(out) samples) times gain into
// out and returns the number of samples consumed. Consumer only.
func (q *ring) mixInto(out []float32, gain float32) int {
	r := q.r.Load()
	n := min(int(q.w.Load()-r), len(out))
	if n <= 0 {
		return 0
	}
	i := int(r % uint64(len(q.buf)))
	first := min(n, len(q.buf)-i)
	peak := q.peak
	for k, v := range q.buf[i : i+first] {
		out[k] += v * gain
		peak = max(peak, abs32(v))
	}
	for k, v := range q.buf[:n-first] {
		out[first+k] += v * gain
		peak = max(peak, abs32(v))
	}
	q.peak = peak
	q.r.Store(r + uint64(n))
	return n
}

// chimeNote is one note of the test sound.
type chimeNote struct {
	freq  float64 // Hz
	start float64 // seconds
	decay float64 // seconds for the note to fall to 1/e
	gain  float64
	pan   float64 // -1 left … +1 right
	bass  bool    // a round bass "bubble" instead of a mallet pluck
}

// midiHz is the frequency of MIDI note n (69 = A4 = 440 Hz).
func midiHz(n int) float64 { return 440 * math.Pow(2, float64(n-69)/12) }

// chimeNotes are a short, playful phrase in C major: a rising arpeggio, a
// little bounce, then a soft chord that rings out, over a round bass.
var chimeNotes = []chimeNote{
	// Bass: C, a bounce down to G, back to C under the chord.
	{freq: midiHz(48), start: 0.00, decay: 0.30, gain: 0.55, bass: true},
	{freq: midiHz(43), start: 0.62, decay: 0.28, gain: 0.50, bass: true},
	{freq: midiHz(48), start: 1.30, decay: 0.75, gain: 0.55, bass: true},
	// Arpeggio up, bouncing left and right.
	{freq: midiHz(72), start: 0.00, decay: 0.30, gain: 0.9, pan: -0.4},
	{freq: midiHz(76), start: 0.17, decay: 0.30, gain: 0.8, pan: 0.4},
	{freq: midiHz(79), start: 0.34, decay: 0.34, gain: 0.8, pan: -0.2},
	// The bounce: a step back and up again.
	{freq: midiHz(76), start: 0.62, decay: 0.26, gain: 0.7, pan: 0.3},
	{freq: midiHz(79), start: 0.79, decay: 0.26, gain: 0.7, pan: -0.3},
	{freq: midiHz(81), start: 0.96, decay: 0.30, gain: 0.75, pan: 0.15},
	// The chord settles, slightly spread.
	{freq: midiHz(64), start: 1.30, decay: 0.70, gain: 0.55, pan: -0.25},
	{freq: midiHz(67), start: 1.32, decay: 0.70, gain: 0.5, pan: 0.25},
	{freq: midiHz(72), start: 1.34, decay: 0.75, gain: 0.6, pan: 0},
}

// chimeLength is the test sound's length; the last 0.3 s fade out.
const chimeLength = 3.0

// ChimeSource generates the test sound: a short, playful phrase of soft
// mallet notes over a round bass, about 3 s, long enough to watch it move
// through the panel's audio flow. The notes are mostly sine with a little
// second harmonic and a quiet, quickly fading "tine" for the attack, and
// the mix runs through a gentle low-pass, so it stays mellow, not shrill.
func ChimeSource(rate int) *Source {
	const peak = 0.30
	sr := float64(rate)
	frames := int(chimeLength * sr)
	pcm := make([]float32, frames*2)
	for _, n := range chimeNotes {
		first := int(n.start * sr)
		angle := (n.pan + 1) * math.Pi / 4 // equal-power pan
		gl, gr := math.Cos(angle)*n.gain, math.Sin(angle)*n.gain
		attack := 0.006 * sr
		phase := 0.0
		for i := 0; first+i < frames; i++ {
			t := float64(i) / sr
			env := math.Exp(-t/n.decay) * min(1, float64(i)/attack)
			if env < 1e-4 && t > 0.05 {
				break
			}
			var v float64
			if n.bass {
				// A soft pitch drop at the start makes it bounce.
				freq := n.freq * (1 + 0.12*math.Exp(-t/0.025))
				phase += 2 * math.Pi * freq / sr
				v = math.Sin(phase) + 0.15*math.Sin(2*phase)*math.Exp(-t/0.08)
			} else {
				phase += 2 * math.Pi * n.freq / sr
				v = math.Sin(phase) +
					0.20*math.Sin(2*phase)*math.Exp(-t/0.10) + // a little body
					0.07*math.Sin(4.07*phase)*math.Exp(-t/0.025) // the tine: soft attack
			}
			v *= env
			pcm[2*(first+i)] += float32(v * gl)
			pcm[2*(first+i)+1] += float32(v * gr)
		}
	}
	// Mellow: a one-pole low-pass at about 2.4 kHz per channel, then a fade
	// to silence over the last 0.3 s.
	a := math.Exp(-2 * math.Pi * 2400 / sr)
	var yl, yr float64
	fade := int(0.3 * sr)
	for i := 0; i < frames; i++ {
		yl = (1-a)*float64(pcm[2*i]) + a*yl
		yr = (1-a)*float64(pcm[2*i+1]) + a*yr
		g := 1.0
		if rest := frames - i; rest < fade {
			g = float64(rest) / float64(fade)
			g *= g
		}
		pcm[2*i], pcm[2*i+1] = float32(yl*g), float32(yr*g)
	}
	var m float32
	for _, v := range pcm {
		m = max(m, v, -v)
	}
	if m > 0 {
		for i := range pcm {
			pcm[i] *= peak / m
		}
	}
	return NewStaticSource("test sound", rate, pcm)
}

// ToneSource generates a sine beep with short fades, for testing without files.
func ToneSource(freq float64, d time.Duration, rate int) *Source {
	frames := int(d.Seconds() * float64(rate))
	fade := rate / 100 // 10 ms
	pcm := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		amp := 0.3
		if i < fade {
			amp *= float64(i) / float64(fade)
		} else if frames-i < fade {
			amp *= float64(frames-i) / float64(fade)
		}
		s := float32(amp * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
		pcm[2*i], pcm[2*i+1] = s, s
	}
	return NewStaticSource(fmt.Sprintf("tone %.0f Hz", freq), rate, pcm)
}
