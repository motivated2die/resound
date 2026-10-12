package audio

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Output roles. The monitor is what the user hears, the cable is what other
// people hear (the virtual cable feeding the game/voice-chat microphone).
const (
	RoleMonitor = "monitor"
	RoleCable   = "cable"
)

// Roles lists the roles in display order.
var Roles = []string{RoleMonitor, RoleCable}

// DefaultBuffer is the requested WASAPI buffer length. Smaller means lower
// latency but more risk of crackling on busy machines.
const DefaultBuffer = 20 * time.Millisecond

// OutputStatus is a snapshot of one role for display.
type OutputStatus struct {
	Role    string
	Enabled bool
	Running bool
	Info    OutputInfo
	Err     error
}

// FormatText returns the device format, e.g. "48000 Hz · 2ch · 32-bit float".
func (s OutputStatus) FormatText() string { return s.Info.Format.String() }

// PlayInfo reports what Play did.
type PlayInfo struct {
	VoiceID  uint64
	Name     string
	Length   time.Duration
	Decode   time.Duration // time until enough audio was buffered to start
	Outputs  int           // number of outputs the sound was queued on
	Decoders int           // decoders running for it (one per distinct output sample rate)
}

// Engine owns the output streams. Sounds are decoded from their files while
// they play; a playing sound only holds a small read-ahead buffer per output.
type Engine struct {
	mu      sync.Mutex
	outputs map[string]*Output
	enabled map[string]bool
	errs    map[string]error
	volumes map[string]float32

	nextID atomic.Uint64
	events chan Event
}

// NewEngine creates an engine with no outputs open.
func NewEngine() *Engine {
	return &Engine{
		outputs: map[string]*Output{},
		enabled: map[string]bool{},
		errs:    map[string]error{},
		volumes: map[string]float32{RoleMonitor: 1, RoleCable: 1},
		events:  make(chan Event, 256),
	}
}

// Events delivers playback and failure events. Read it continuously.
func (e *Engine) Events() <-chan Event { return e.events }

// SetOutput (re)opens the stream for role. deviceID "" means the system
// default output. enabled=false closes the role's stream.
func (e *Engine) SetOutput(role, deviceID string, enabled bool) error {
	e.mu.Lock()
	old := e.outputs[role]
	delete(e.outputs, role)
	delete(e.errs, role)
	e.enabled[role] = enabled
	vol := e.volumes[role]
	e.mu.Unlock()

	if old != nil {
		old.Close()
	}
	if !enabled {
		return nil
	}
	out, err := openOutput(role, deviceID, DefaultBuffer, e.events)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.errs[role] = err
		return fmt.Errorf("%s output: %w", role, err)
	}
	out.Mixer.SetVolume(vol)
	e.outputs[role] = out
	return nil
}

// SetVolume sets a role's master volume (0..1).
func (e *Engine) SetVolume(role string, v float32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.volumes[role] = v
	if o := e.outputs[role]; o != nil {
		o.Mixer.SetVolume(v)
	}
}

// PlayOptions selects where and how loud a sound plays.
type PlayOptions struct {
	// Roles limits playback to these outputs (nil = every running output).
	// A preview uses []string{RoleMonitor}.
	Roles []string
	// Volume overrides a role's master volume for this sound (0..2).
	// Roles without an entry use the master volume.
	Volume map[string]float32
}

// SoundRef identifies a sound file to play.
type SoundRef struct {
	Path string
	// Length is the known duration (from the library), used for progress
	// until the decoder reaches the end. 0 = the decoder's estimate.
	Length time.Duration
	// OnDecoded, if set, receives the exact length once the file was
	// decoded to its end (called from a producer goroutine).
	OnDecoded func(time.Duration)
}

// Prepared is a sound buffered and ready to start on the outputs that were
// running when it was prepared. Start it, or Close it to release it.
type Prepared struct {
	name     string
	length   time.Duration
	decode   time.Duration
	decoders int
	mu       sync.Mutex
	sources  map[*Output]*Source
}

// Close releases the sources that were not started.
func (p *Prepared) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for o, s := range p.sources {
		s.Close()
		delete(p.sources, o)
	}
}

// PrepareSound opens a sound for every running output in roles (nil = all)
// and waits until enough audio is decoded for a clean start. Outputs with
// the same sample rate share one decoder. Do this before muting anything.
func (e *Engine) PrepareSound(ref SoundRef, roles []string) (*Prepared, error) {
	return e.prepare(roles, func(rate, n int) ([]*Source, error) {
		var onEnd func(int64)
		if ref.OnDecoded != nil {
			onEnd = func(frames int64) { ref.OnDecoded(time.Duration(frames) * time.Second / time.Duration(rate)) }
		}
		length := int64(ref.Length) * int64(rate) / int64(time.Second)
		return openFileStreams(ref.Path, rate, n, length, onEnd)
	})
}

// PrepareFile prepares a file (test sounds).
func (e *Engine) PrepareFile(path string) (*Prepared, error) {
	return e.PrepareSound(SoundRef{Path: path}, nil)
}

// PrepareTone prepares the short test chime (ChimeSource).
func (e *Engine) PrepareTone() (*Prepared, error) {
	return e.prepare(nil, func(rate, n int) ([]*Source, error) {
		var out []*Source
		for range n {
			out = append(out, ChimeSource(rate))
		}
		return out, nil
	})
}

// prepare opens n sources per distinct output rate and waits for them.
func (e *Engine) prepare(roles []string, open func(rate, n int) ([]*Source, error)) (*Prepared, error) {
	byRate := map[int][]*Output{}
	var rates []int
	for _, o := range e.liveOutputs() {
		if roles != nil && !slices.Contains(roles, o.role) {
			continue
		}
		r := o.Info().Format.Rate
		if byRate[r] == nil {
			rates = append(rates, r)
		}
		byRate[r] = append(byRate[r], o)
	}
	if len(rates) == 0 {
		return nil, errors.New("no output is running")
	}
	start := time.Now()
	p := &Prepared{sources: map[*Output]*Source{}}
	for _, r := range rates { // decoders start in parallel
		outs := byRate[r]
		srcs, err := open(r, len(outs))
		if err != nil {
			p.Close()
			return nil, err
		}
		for i, o := range outs {
			p.sources[o] = srcs[i]
		}
		p.decoders++
	}
	for _, s := range p.sources {
		if err := s.waitReady(); err != nil {
			p.Close()
			return nil, err
		}
		p.name, p.length = s.Name, s.Length()
	}
	p.decode = time.Since(start)
	return p, nil
}

// Start queues a prepared sound. All copies are queued back to back, so they
// start in the same device period. Copies for outputs that stopped or are
// not in opts.Roles are released.
func (e *Engine) Start(p *Prepared, opts PlayOptions) (PlayInfo, error) {
	info := PlayInfo{VoiceID: e.nextID.Add(1), Name: p.name, Length: p.length, Decode: p.decode, Decoders: p.decoders}
	queuedAt := time.Now()
	p.mu.Lock()
	for _, o := range e.liveOutputs() {
		src, ok := p.sources[o]
		if !ok || (opts.Roles != nil && !slices.Contains(opts.Roles, o.role)) {
			continue
		}
		gain, absolute := float32(1), false
		if v, ok := opts.Volume[o.role]; ok {
			gain, absolute = v, true
		}
		o.Mixer.Add(info.VoiceID, src, gain, absolute, queuedAt)
		delete(p.sources, o) // the mixer owns it now
		info.Outputs++
	}
	p.mu.Unlock()
	p.Close()
	if info.Outputs == 0 {
		return PlayInfo{}, errors.New("no matching output is running")
	}
	return info, nil
}

// PlayFile prepares and starts a file on every running output.
func (e *Engine) PlayFile(path string) (PlayInfo, error) {
	p, err := e.PrepareFile(path)
	if err != nil {
		return PlayInfo{}, err
	}
	return e.Start(p, PlayOptions{})
}

// PlayTone plays the test chime on every running output.
func (e *Engine) PlayTone() (PlayInfo, error) {
	p, err := e.PrepareTone()
	if err != nil {
		return PlayInfo{}, err
	}
	return e.Start(p, PlayOptions{})
}

// Voices returns the playing voices, as seen by the monitor output (or the
// cable output when the monitor is not running).
func (e *Engine) Voices() []VoiceState {
	outs := e.liveOutputs()
	if len(outs) == 0 {
		return nil
	}
	return outs[0].Mixer.Snapshot()
}

// SetPaused pauses or resumes a voice on every output.
func (e *Engine) SetPaused(id uint64, paused bool) {
	for _, o := range e.liveOutputs() {
		o.Mixer.SetPaused(id, paused)
	}
}

// SetVoiceVolume changes a playing voice's volume on every output, like
// PlayOptions.Volume does at the start: roles in vol get that gain, the
// others follow the master volume again.
func (e *Engine) SetVoiceVolume(id uint64, vol map[string]float32) {
	for _, o := range e.liveOutputs() {
		gain, absolute := float32(1), false
		if v, ok := vol[o.role]; ok {
			gain, absolute = v, true
		}
		o.Mixer.SetGain(id, gain, absolute)
	}
}

// StopAll stops every voice on every output.
func (e *Engine) StopAll() {
	for _, o := range e.liveOutputs() {
		o.Mixer.StopAll()
	}
}

// Status returns one entry per role, in display order.
func (e *Engine) Status() []OutputStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []OutputStatus
	for _, role := range Roles {
		s := OutputStatus{Role: role, Enabled: e.enabled[role], Err: e.errs[role]}
		if o := e.outputs[role]; o != nil {
			s.Info = o.Info()
			if failed, err := o.Failed(); failed {
				s.Err = err
			} else {
				s.Running = true
			}
		}
		out = append(out, s)
	}
	return out
}

// Close stops all streams.
func (e *Engine) Close() {
	e.mu.Lock()
	outs := e.outputs
	e.outputs = map[string]*Output{}
	e.mu.Unlock()
	for _, o := range outs {
		o.Close()
	}
}

func (e *Engine) liveOutputs() []*Output {
	e.mu.Lock()
	defer e.mu.Unlock()
	var outs []*Output
	for _, role := range Roles {
		if o := e.outputs[role]; o != nil {
			if failed, _ := o.Failed(); !failed {
				outs = append(outs, o)
			}
		}
	}
	return outs
}
