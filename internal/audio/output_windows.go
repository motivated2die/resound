package audio

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
	"golang.org/x/sys/windows"

	"github.com/motivated2die/resound/internal/wincom"
)

// OutputInfo describes an opened device stream.
type OutputInfo struct {
	DeviceID      string
	DeviceName    string
	Format        sampleFormat
	BufferFrames  int
	Buffer        time.Duration // size of the WASAPI buffer we keep filled
	StreamLatency time.Duration // latency reported by the audio engine
}

// Output is one long-lived shared-mode WASAPI render stream with its own
// mixer. Sounds are added to the mixer; the stream itself never restarts
// per sound, so playback starts within one buffer period.
type Output struct {
	role   string
	Mixer  *Mixer
	info   OutputInfo
	events chan<- Event

	failed   atomic.Bool
	failErr  atomic.Value // error
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// openOutput starts a render thread for deviceID ("" = default output) and
// waits until the stream runs or fails to start.
func openOutput(role, deviceID string, bufferDur time.Duration, events chan<- Event) (*Output, error) {
	o := &Output{
		role:   role,
		Mixer:  NewMixer(role, events),
		events: events,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	o.info.DeviceID = deviceID
	ready := make(chan error, 1)
	go o.run(ready, bufferDur)
	if err := <-ready; err != nil {
		<-o.done
		return nil, err
	}
	return o, nil
}

// Info returns the stream description (immutable once opened).
func (o *Output) Info() OutputInfo { return o.info }

// Failed reports whether the stream died after starting, and why.
func (o *Output) Failed() (bool, error) {
	if !o.failed.Load() {
		return false, nil
	}
	err, _ := o.failErr.Load().(error)
	return true, err
}

// Close stops the stream and waits for the render thread to exit.
func (o *Output) Close() {
	o.stopOnce.Do(func() { close(o.stop) })
	<-o.done
}

func (o *Output) fail(err error) {
	o.failErr.Store(err)
	o.failed.Store(true)
	select {
	case o.events <- Event{Kind: EventOutputFailed, Output: o.role, Err: err}:
	default:
	}
}

// run owns every COM object of this stream. The OS thread stays locked for
// the goroutine's lifetime and is discarded when it returns.
func (o *Output) run(ready chan<- error, bufferDur time.Duration) {
	runtime.LockOSThread()
	defer close(o.done)
	defer o.Mixer.Drain() // stop the voices' producers and close their files

	started := false
	// Errors name the device (once found) and the step, e.g. `"Speakers
	// (Realtek)": Initialize: another app is using this device exclusively
	// (AUDCLNT_E_DEVICE_IN_USE)`; Problem shortens them for the panel.
	fail := func(step string, err error) {
		err = fmt.Errorf("%s: %w", step, describeHRESULT(err))
		if o.info.DeviceName != "" {
			err = fmt.Errorf("%q: %w", o.info.DeviceName, err)
		}
		if !started {
			ready <- err
			return
		}
		o.fail(err)
	}

	if err := wincom.Init(); err != nil {
		fail("CoInitializeEx", err)
		return
	}
	defer ole.CoUninitialize()
	defer boostThreadPriority()()

	enum, err := wincom.NewEnumerator()
	if err != nil {
		fail("enumerator", err)
		return
	}
	defer enum.Release()

	dev, err := wincom.Find(enum, wincom.Render, o.info.DeviceID)
	if err != nil {
		if errors.Is(err, wincom.ErrNotFound) {
			err = ErrDeviceGone
		}
		fail("finding device", err)
		return
	}
	defer dev.Release()
	if o.info.DeviceID == "" {
		_ = dev.GetId(&o.info.DeviceID)
	}
	o.info.DeviceName = wincom.FriendlyName(dev)

	var ac *wca.IAudioClient
	if err := wincom.Activate(dev, wca.IID_IAudioClient, unsafe.Pointer(&ac)); err != nil {
		fail("activate", err)
		return
	}
	defer ac.Release()

	var wfx *wca.WAVEFORMATEX
	if err := ac.GetMixFormat(&wfx); err != nil {
		fail("GetMixFormat", err)
		return
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(wfx)))
	format, err := parseFormat(wfx)
	if err != nil {
		fail("mix format", err)
		return
	}
	o.info.Format = format

	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		fail("CreateEvent", err)
		return
	}
	defer windows.CloseHandle(event)

	hns := wca.REFERENCE_TIME(bufferDur / 100) // REFERENCE_TIME is in 100 ns units
	if err := ac.Initialize(wca.AUDCLNT_SHAREMODE_SHARED,
		wca.AUDCLNT_STREAMFLAGS_EVENTCALLBACK|wca.AUDCLNT_STREAMFLAGS_NOPERSIST,
		hns, 0, wfx, nil); err != nil {
		fail("Initialize", err)
		return
	}
	if err := ac.SetEventHandle(uintptr(event)); err != nil {
		fail("SetEventHandle", err)
		return
	}
	var bufFrames uint32
	if err := ac.GetBufferSize(&bufFrames); err != nil {
		fail("GetBufferSize", err)
		return
	}
	var latency wca.REFERENCE_TIME
	_ = ac.GetStreamLatency(&latency)

	var rc *wca.IAudioRenderClient
	if err := ac.GetService(wca.IID_IAudioRenderClient, &rc); err != nil {
		fail("GetService(IAudioRenderClient)", err)
		return
	}
	defer rc.Release()

	// Prefill one buffer of silence so the stream starts cleanly.
	var data *byte
	if err := rc.GetBuffer(bufFrames, &data); err != nil {
		fail("GetBuffer (prefill)", err)
		return
	}
	if err := rc.ReleaseBuffer(bufFrames, wca.AUDCLNT_BUFFERFLAGS_SILENT); err != nil {
		fail("ReleaseBuffer (prefill)", err)
		return
	}
	if err := ac.Start(); err != nil {
		fail("Start", err)
		return
	}
	defer ac.Stop()

	o.info.BufferFrames = int(bufFrames)
	o.info.Buffer = time.Duration(bufFrames) * time.Second / time.Duration(format.Rate)
	o.info.StreamLatency = time.Duration(latency) * 100
	started = true
	ready <- nil

	scratch := make([]float32, int(bufFrames)*2)
	for {
		select {
		case <-o.stop:
			return
		default:
		}
		r, err := windows.WaitForSingleObject(event, 200)
		if err != nil {
			fail("WaitForSingleObject", err)
			return
		}
		if r == uint32(windows.WAIT_TIMEOUT) {
			continue // no period elapsed; loop to check stop
		}
		var padding uint32
		if err := ac.GetCurrentPadding(&padding); err != nil {
			fail("GetCurrentPadding", err)
			return
		}
		n := bufFrames - padding
		if n == 0 {
			continue
		}
		if err := rc.GetBuffer(n, &data); err != nil {
			fail("GetBuffer", err)
			return
		}
		mix := scratch[:n*2]
		o.Mixer.Read(mix)
		writeSamples(unsafe.Slice(data, int(n)*format.BlockAlign), mix, format)
		if err := rc.ReleaseBuffer(n, 0); err != nil {
			fail("ReleaseBuffer", err)
			return
		}
	}
}

// writeSamples converts interleaved stereo float32 into the device format.
// Mono devices get (L+R)/2; devices with more than two channels get L/R on
// the first two and silence on the rest.
func writeSamples(dst []byte, mix []float32, f sampleFormat) {
	frames := len(mix) / 2
	ch := f.Channels
	sample := func(frame, c int) float32 {
		l, r := mix[2*frame], mix[2*frame+1]
		switch {
		case ch == 1:
			return (l + r) / 2
		case c == 0:
			return l
		case c == 1:
			return r
		}
		return 0
	}
	switch {
	case f.Float:
		out := unsafe.Slice((*float32)(unsafe.Pointer(&dst[0])), frames*ch)
		if ch == 2 {
			copy(out, mix)
			return
		}
		for i := 0; i < frames; i++ {
			for c := 0; c < ch; c++ {
				out[i*ch+c] = sample(i, c)
			}
		}
	case f.Bits == 16:
		out := unsafe.Slice((*int16)(unsafe.Pointer(&dst[0])), frames*ch)
		for i := 0; i < frames; i++ {
			for c := 0; c < ch; c++ {
				out[i*ch+c] = int16(math.Round(float64(sample(i, c)) * math.MaxInt16))
			}
		}
	case f.Bits == 32:
		out := unsafe.Slice((*int32)(unsafe.Pointer(&dst[0])), frames*ch)
		for i := 0; i < frames; i++ {
			for c := 0; c < ch; c++ {
				out[i*ch+c] = int32(math.Round(float64(sample(i, c)) * math.MaxInt32))
			}
		}
	case f.Bits == 24:
		for i := 0; i < frames; i++ {
			for c := 0; c < ch; c++ {
				v := int32(math.Round(float64(sample(i, c)) * 8388607))
				p := (i*ch + c) * 3
				dst[p], dst[p+1], dst[p+2] = byte(v), byte(v>>8), byte(v>>16)
			}
		}
	}
}
