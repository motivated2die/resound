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

// chimeNote is one blip of the test chime.
type chimeNote struct {
	from, to   float64 // Hz; the pitch bends from → to in the first 40 ms
	start, dur float64 // seconds
	pan        float64 // -1 left … +1 right
}

// chimeNotes rise G5 → D6 → G6, bouncing left, right, centre.
var chimeNotes = []chimeNote{
	{from: 740, to: 784, start: 0, dur: 0.11, pan: -0.45},
	{from: 1109, to: 1175, start: 0.075, dur: 0.11, pan: 0.45},
	{from: 1480, to: 1568, start: 0.15, dur: 0.30, pan: 0},
}

// ChimeSource generates the test sound: three quick rising FM blips that
// bend up in pitch and bounce across the stereo field, about 0.45 s. The
// modulation fades within each note, so every blip starts bright and
// settles into a pure tone.
func ChimeSource(rate int) *Source {
	const peak = 0.32
	end := 0.0
	for _, n := range chimeNotes {
		end = max(end, n.start+n.dur)
	}
	frames := int(end * float64(rate))
	pcm := make([]float32, frames*2)
	sr := float64(rate)
	for _, n := range chimeNotes {
		first := int(n.start * sr)
		count := min(int(n.dur*sr), frames-first)
		// Equal-power pan.
		angle := (n.pan + 1) * math.Pi / 4
		gl, gr := math.Cos(angle), math.Sin(angle)
		attack, release := 0.004*sr, 0.006*sr
		phase := 0.0
		for i := range count {
			t := float64(i) / sr
			bend := min(1, t/0.04)
			freq := n.from + (n.to-n.from)*(1-(1-bend)*(1-bend)) // ease-out
			phase += 2 * math.Pi * freq / sr
			index := 2.2 * math.Exp(-t/0.035) // FM depth: bright attack, pure tail
			s := math.Sin(phase + index*math.Sin(2*phase))
			env := math.Exp(-t / (n.dur / 3))
			env *= min(1, float64(i)/attack, float64(count-i)/release)
			v := s * env
			pcm[2*(first+i)] += float32(v * gl)
			pcm[2*(first+i)+1] += float32(v * gr)
		}
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
	return NewStaticSource("test chime", rate, pcm)
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
