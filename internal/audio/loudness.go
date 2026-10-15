package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gopxl/beep/v2"
	gomp3 "github.com/hajimehoshi/go-mp3"
)

// Loudness normalization: every sound is measured once (by the library's
// indexer) and played with the gain that brings it to LoudnessTarget, so
// sounds from different sources play about equally loud. The volumes
// (defaults and per-sound) apply on top.
//
// The measure is the integrated loudness of ITU-R BS.1770-4 (as in EBU
// R 128): K-weighted energy (a filter modelling how loud frequencies sound)
// over 400 ms blocks, averaged over the blocks that aren't silence or much
// quieter than the rest. It reflects the whole sound, not its loudest
// sample: a sharp impact in an otherwise quiet sound barely moves it.
//
// Quiet sounds are boosted by at most maxBoostDB, and only as far as their
// robust peak (the level only the loudest peakShare of the sound exceeds)
// stays at full scale. Single transients above it are rounded off by the
// mixer's soft clipper instead of limiting the whole sound's gain.
const (
	// LoudnessTarget is the loudness sounds are normalized to, in LUFS.
	// Most sounds from soundboard sites are louder (−8 to −14 LUFS).
	LoudnessTarget = -16.0
	// LoudnessVersion identifies the measurement; stored measurements of
	// another version are measured again.
	LoudnessVersion = 1
	// SilentLUFS is the loudness reported for silence.
	SilentLUFS = -70.0

	maxBoostDB = 12.0
	minGainDB  = -30.0
	// peakShare is the share of frames allowed above the robust peak:
	// 1 ms per second, the length of a sharp transient.
	peakShare = 0.001

	// Files longer than sampleAbove (songs, recordings) are measured in
	// segments of segmentLength spread evenly over the file, the first at
	// its start and the last at its end: about as accurate as measuring all
	// of it, in a fraction of the time.
	sampleAbove   = 90 * time.Second
	segments      = 6
	segmentLength = 10 * time.Second
	// maxMeasure bounds the audio measured of files that can't seek.
	maxMeasure = 10 * time.Minute
	// maxInMemory is the largest MP3 read into memory for measuring.
	maxInMemory = 64 << 20
)

// Loudness is a sound file's measured level.
type Loudness struct {
	LUFS float64 // integrated loudness; SilentLUFS for silence
	Peak float64 // robust peak in dBFS (see peakShare)
	// Length is the file's length, exact when Complete.
	Length   time.Duration
	Complete bool // the whole file was decoded
}

// NormGain returns the linear gain that normalizes a sound with this
// loudness and robust peak (dBFS).
func NormGain(lufs, peak float64) float32 {
	if lufs <= SilentLUFS {
		return 1
	}
	db := LoudnessTarget - lufs
	if db > 0 {
		db = min(db, maxBoostDB, max(0, -peak))
	}
	return float32(math.Pow(10, max(db, minGainDB)/20))
}

// MeasureLoudness decodes a sound file at its own sample rate and measures
// its loudness. Files longer than sampleAbove are measured in segments
// spread over the whole file. Files that can't be read give an error that
// matches ErrUnreadable.
func MeasureLoudness(path string) (Loudness, error) {
	stream, rate, closer, err := openMeasureStream(path)
	if err != nil {
		return Loudness{}, err
	}
	defer closer()
	l, err := measure(stream, rate)
	if err == nil {
		err = stream.Err()
	}
	if err != nil {
		return Loudness{}, fmt.Errorf("measuring %s: %w: %w", filepath.Base(path), err, ErrUnreadable)
	}
	return l, nil
}

// seekStreamer is a decoder that knows its length and can seek (in frames).
type seekStreamer interface {
	beep.Streamer
	Len() int
	Seek(frame int) error
	Err() error
}

// openMeasureStream opens a sound file for measuring. MP3 files are decoded
// by go-mp3 directly, from memory: beep's MP3 decoder converts every sample
// with math.Pow (a fifth of the decoding time), and an in-memory file lets
// go-mp3 index its frames for seeking without thousands of small reads.
func openMeasureStream(path string) (seekStreamer, int, func(), error) {
	if strings.EqualFold(filepath.Ext(path), ".mp3") {
		if info, err := os.Stat(path); err == nil && info.Size() <= maxInMemory {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, 0, nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
			}
			d, err := gomp3.NewDecoder(bytes.NewReader(data))
			if err != nil {
				return nil, 0, nil, fmt.Errorf("decoding %s: %w: %w", filepath.Base(path), err, ErrUnreadable)
			}
			return &mp3Stream{d: d}, d.SampleRate(), func() {}, nil
		}
	}
	stream, format, closer, err := openStream(path)
	if err != nil {
		return nil, 0, nil, err
	}
	return stream, int(format.SampleRate), closer, nil
}

// mp3Stream reads go-mp3's output (16-bit stereo, even for mono files) as
// frames.
type mp3Stream struct {
	d   *gomp3.Decoder
	buf []byte
	err error
}

func (m *mp3Stream) Stream(out [][2]float64) (int, bool) {
	if m.err != nil {
		return 0, false
	}
	if need := 4 * len(out); len(m.buf) < need {
		m.buf = make([]byte, need)
	}
	b := m.buf[:4*len(out)]
	n, err := io.ReadFull(m.d, b)
	frames := n / 4
	for i := range frames {
		f := b[4*i : 4*i+4]
		out[i] = [2]float64{
			float64(int16(binary.LittleEndian.Uint16(f))) / 32768,
			float64(int16(binary.LittleEndian.Uint16(f[2:]))) / 32768,
		}
	}
	if err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			m.err = err
		}
		return frames, frames > 0
	}
	return frames, true
}

func (m *mp3Stream) Len() int { return int(max(m.d.Length(), 0) / 4) }

func (m *mp3Stream) Seek(frame int) error {
	_, err := m.d.Seek(int64(frame)*4, io.SeekStart)
	return err
}

func (m *mp3Stream) Err() error { return m.err }

// biquad is a second-order IIR filter section (a0 normalized to 1).
type biquad struct{ b0, b1, b2, a1, a2 float64 }

// kWeighting returns BS.1770's two filter stages (high shelf, then high
// pass) for a sample rate. The standard lists coefficients for 48 kHz
// only; these are derived from the analog prototypes as libebur128 does,
// and match them at 48 kHz.
func kWeighting(rate int) (shelf, highPass biquad) {
	fs := float64(rate)

	f0, gain, q := 1681.974450955533, 3.999843853973347, 0.7071752369554196
	k := math.Tan(math.Pi * f0 / fs)
	vh := math.Pow(10, gain/20)
	vb := math.Pow(vh, 0.4996667741545416)
	a0 := 1 + k/q + k*k
	shelf = biquad{
		b0: (vh + vb*k/q + k*k) / a0,
		b1: 2 * (k*k - vh) / a0,
		b2: (vh - vb*k/q + k*k) / a0,
		a1: 2 * (k*k - 1) / a0,
		a2: (1 - k/q + k*k) / a0,
	}

	f0, q = 38.13547087602444, 0.5003270373238773
	k = math.Tan(math.Pi * f0 / fs)
	a0 = 1 + k/q + k*k
	highPass = biquad{b0: 1, b1: -2, b2: 1, a1: 2 * (k*k - 1) / a0, a2: (1 - k/q + k*k) / a0}
	return shelf, highPass
}

// measure measures a stream (stereo, at rate). A stream that can seek and
// is longer than sampleAbove is measured in segments spread over it; others
// are measured whole, up to maxMeasure.
func measure(s beep.Streamer, rate int) (Loudness, error) {
	if rate <= 0 {
		return Loudness{}, errors.New("unknown sample rate")
	}
	m := newMeter(rate)
	if ss, ok := s.(seekStreamer); ok {
		total := ss.Len()
		seg := int(segmentLength.Seconds() * float64(rate))
		if total > int(sampleAbove.Seconds()*float64(rate)) {
			for i := range segments {
				if err := ss.Seek((total - seg) * i / (segments - 1)); err != nil {
					return Loudness{}, err
				}
				m.read(s, seg)
			}
			return m.result(total, false)
		}
	}
	complete := m.read(s, int(maxMeasure.Seconds()*float64(rate)))
	return m.result(m.frames, complete)
}

// meter accumulates the K-weighted energy and peak levels of a stream.
type meter struct {
	rate      int
	shelf, hp biquad
	// Filter state per channel, transposed direct form II.
	s1, s2, h1, h2 [2]float64
	sub            int       // frames per 100 ms
	subs           []float64 // K-weighted energy per 100 ms, both channels
	acc            float64   // energy of the current 100 ms so far
	inSub, frames  int
	hist           [2200]int // frames by peak level, in 1/16-octave steps
	buf            [][2]float64
}

func newMeter(rate int) *meter {
	m := &meter{rate: rate, sub: max(1, rate/10), buf: make([][2]float64, 4096)}
	m.shelf, m.hp = kWeighting(rate)
	return m
}

// read adds up to limit frames of s and reports whether s ended.
func (m *meter) read(s beep.Streamer, limit int) bool {
	sh, hp := m.shelf, m.hp
	for done := 0; done < limit; {
		n, ok := s.Stream(m.buf[:min(len(m.buf), limit-done)])
		for _, fr := range m.buf[:n] {
			for c, x := range fr {
				y := sh.b0*x + m.s1[c]
				m.s1[c] = sh.b1*x - sh.a1*y + m.s2[c]
				m.s2[c] = sh.b2*x - sh.a2*y
				z := y + m.h1[c] // high pass: b0 = 1, b1 = −2, b2 = 1
				m.h1[c] = -2*y - hp.a1*z + m.h2[c]
				m.h2[c] = y - hp.a2*z
				m.acc += z * z
			}
			// The float's exponent and top four mantissa bits put the
			// frame's peak into a bin, without a logarithm per sample.
			p := float32(max(math.Abs(fr[0]), math.Abs(fr[1])))
			m.hist[min(int(math.Float32bits(p)>>19), len(m.hist)-1)]++
			if m.inSub++; m.inSub == m.sub {
				m.subs = append(m.subs, m.acc)
				m.acc, m.inSub = 0, 0
			}
		}
		done += n
		m.frames += n
		if !ok || n == 0 {
			return true
		}
	}
	return false
}

// result is the measurement, for a stream of length frames.
func (m *meter) result(length int, complete bool) (Loudness, error) {
	if m.frames == 0 {
		return Loudness{}, errors.New("no audio")
	}
	return Loudness{
		LUFS:     gatedLoudness(m.subs, m.acc, m.sub, m.frames),
		Peak:     robustPeak(m.hist[:], m.frames),
		Length:   time.Duration(length) * time.Second / time.Duration(m.rate),
		Complete: complete,
	}, nil
}

func lufs(meanSquare float64) float64 { return -0.691 + 10*math.Log10(meanSquare) }

// gatedLoudness applies BS.1770-4's gating to the 100 ms energies: blocks
// below −70 LUFS are silence; of the rest, blocks more than 10 LU below
// their average don't count. Sounds shorter than one 400 ms block (common
// on a soundboard) are measured as one block, tail included.
func gatedLoudness(subs []float64, tail float64, sub, frames int) float64 {
	if len(subs) < 4 {
		total := tail
		for _, e := range subs {
			total += e
		}
		if total <= 0 {
			return SilentLUFS
		}
		return max(SilentLUFS, lufs(total/float64(frames)))
	}
	blocks := make([]float64, 0, len(subs)-3)
	for i := 3; i < len(subs); i++ {
		blocks = append(blocks, (subs[i-3]+subs[i-2]+subs[i-1]+subs[i])/float64(4*sub))
	}
	gate := func(threshold float64) (mean float64, n int) {
		sum := 0.0
		for _, b := range blocks {
			if b > 0 && lufs(b) > threshold {
				sum += b
				n++
			}
		}
		if n == 0 {
			return 0, 0
		}
		return sum / float64(n), n
	}
	mean, n := gate(SilentLUFS)
	if n == 0 {
		return SilentLUFS
	}
	if mean, n = gate(max(SilentLUFS, lufs(mean)-10)); n == 0 {
		return SilentLUFS
	}
	return lufs(mean)
}

// robustPeak returns the level in dBFS that only peakShare of the frames
// exceed, from frame counts by level bin (the bin's upper edge, so at most
// 0.4 dB above the exact level).
func robustPeak(hist []int, frames int) float64 {
	allowed := int(float64(frames) * peakShare)
	above := 0
	for i := len(hist) - 1; i > 0; i-- {
		if above += hist[i]; above > allowed {
			top := math.Float32frombits(uint32(i+1) << 19)
			return 20 * math.Log10(float64(top))
		}
	}
	return -120
}
