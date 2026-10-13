package audio

import (
	"errors"
	"fmt"
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

// Live level meters for devices outside Resound's own mixers, shown while
// the panel's audio flow is visible:
//
//   - InputLevel records from a microphone only to measure it. Windows
//     meters a capture device only while some app records from it, so
//     Resound has to; nothing is kept. While it runs, Windows 11 shows its
//     microphone-in-use icon.
//   - DeviceLevel reads Windows' own peak meter of an output device: the
//     mix of everything played on it (for VB-Cable's "CABLE Input": the
//     sounds and the routed microphone). Output meters need no stream.
//
// Each runs on its own locked COM thread that owns its COM objects, like an
// output's render thread.

// ErrMicBlocked means Windows' privacy settings keep desktop apps from the
// microphone.
var ErrMicBlocked = errors.New("Windows' privacy settings block microphone access for desktop apps")

// meterThread is the thread and peak shared by both meters.
type meterThread struct {
	peak    peakHold
	failed  atomic.Bool
	failErr atomic.Value // error
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func newMeterThread() *meterThread {
	return &meterThread{stop: make(chan struct{}), done: make(chan struct{})}
}

// Take returns the peak (0..1) since the last call, and why the meter
// stopped working after it started, if it did.
func (m *meterThread) Take() (float32, error) {
	if m.failed.Load() {
		err, _ := m.failErr.Load().(error)
		return 0, err
	}
	return m.peak.take(), nil
}

// Close stops the meter and waits for its thread.
func (m *meterThread) Close() {
	m.once.Do(func() { close(m.stop) })
	<-m.done
}

func (m *meterThread) fail(err error) {
	m.failErr.Store(err)
	m.failed.Store(true)
}

// start runs body on a locked COM thread and waits until it signalled ready
// or failed before that.
func (m *meterThread) start(body func(ready chan<- error)) error {
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer close(m.done)
		if err := wincom.Init(); err != nil {
			ready <- fmt.Errorf("CoInitializeEx: %w", err)
			return
		}
		defer ole.CoUninitialize()
		body(ready)
	}()
	if err := <-ready; err != nil {
		<-m.done
		return err
	}
	return nil
}

// --- microphone ---------------------------------------------------------------

// InputLevel measures a capture device by recording from it.
type InputLevel struct{ *meterThread }

// OpenInputLevel starts recording from the capture device deviceID ("" =
// the default recording device) to measure its level.
func OpenInputLevel(deviceID string) (*InputLevel, error) {
	l := &InputLevel{newMeterThread()}
	if err := l.start(func(ready chan<- error) { l.run(deviceID, ready) }); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *InputLevel) run(deviceID string, ready chan<- error) {
	started := false
	fail := func(step string, err error) {
		if wincom.IsAccessDenied(err) {
			err = ErrMicBlocked
		} else {
			err = fmt.Errorf("%s: %w", step, describeHRESULT(err))
		}
		if !started {
			ready <- err
			return
		}
		l.fail(err)
	}

	enum, err := wincom.NewEnumerator()
	if err != nil {
		fail("enumerator", err)
		return
	}
	defer enum.Release()
	dev, err := wincom.Find(enum, wincom.Capture, deviceID)
	if err != nil {
		fail("finding device", err)
		return
	}
	defer dev.Release()

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
	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		fail("CreateEvent", err)
		return
	}
	defer windows.CloseHandle(event)
	const bufferDur = 50 * time.Millisecond
	if err := ac.Initialize(wca.AUDCLNT_SHAREMODE_SHARED,
		wca.AUDCLNT_STREAMFLAGS_EVENTCALLBACK|wca.AUDCLNT_STREAMFLAGS_NOPERSIST,
		wca.REFERENCE_TIME(bufferDur/100), 0, wfx, nil); err != nil {
		fail("Initialize", err)
		return
	}
	if err := ac.SetEventHandle(uintptr(event)); err != nil {
		fail("SetEventHandle", err)
		return
	}
	var cc *wca.IAudioCaptureClient
	if err := ac.GetService(wca.IID_IAudioCaptureClient, &cc); err != nil {
		fail("GetService(IAudioCaptureClient)", err)
		return
	}
	defer cc.Release()
	if err := ac.Start(); err != nil {
		fail("Start", err)
		return
	}
	defer ac.Stop()
	started = true
	ready <- nil

	for {
		select {
		case <-l.stop:
			return
		default:
		}
		r, err := windows.WaitForSingleObject(event, 200)
		if err != nil {
			fail("WaitForSingleObject", err)
			return
		}
		if r == uint32(windows.WAIT_TIMEOUT) {
			continue
		}
		for {
			var next uint32
			if err := cc.GetNextPacketSize(&next); err != nil {
				fail("GetNextPacketSize", err)
				return
			}
			if next == 0 {
				break
			}
			var (
				data   *byte
				frames uint32
				flags  uint32
			)
			if err := cc.GetBuffer(&data, &frames, &flags, nil, nil); err != nil {
				fail("GetBuffer", err)
				return
			}
			if frames > 0 && flags&wca.AUDCLNT_BUFFERFLAGS_SILENT == 0 {
				l.peak.raise(samplesPeak(unsafe.Slice(data, int(frames)*format.BlockAlign), format))
			}
			if err := cc.ReleaseBuffer(frames); err != nil {
				fail("ReleaseBuffer", err)
				return
			}
		}
	}
}

// samplesPeak returns the loudest sample (0..1) of interleaved device-format
// audio, the reverse of writeSamples.
func samplesPeak(b []byte, f sampleFormat) float32 {
	var peak float32
	switch {
	case f.Float && f.Bits == 32:
		for _, v := range unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4) {
			peak = max(peak, abs32(v))
		}
	case f.Bits == 16:
		for _, v := range unsafe.Slice((*int16)(unsafe.Pointer(&b[0])), len(b)/2) {
			peak = max(peak, abs32(float32(v)/32768))
		}
	case f.Bits == 32:
		for _, v := range unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), len(b)/4) {
			peak = max(peak, abs32(float32(v)/2147483648))
		}
	case f.Bits == 24:
		for p := 0; p+2 < len(b); p += 3 {
			v := int32(uint32(b[p])<<8|uint32(b[p+1])<<16|uint32(b[p+2])<<24) >> 8
			peak = max(peak, abs32(float32(v)/8388608))
		}
	}
	return min(peak, 1)
}

// --- output device ------------------------------------------------------------

// DeviceLevel reads Windows' peak meter of an output device.
type DeviceLevel struct{ *meterThread }

// meterPoll is how often DeviceLevel reads the meter. Windows' meter holds
// the peak of the last device period, so this only needs to be a little
// faster than the panel's repaints.
const meterPoll = 15 * time.Millisecond

// OpenDeviceLevel starts reading the peak meter of the output deviceID
// ("" = the default output).
func OpenDeviceLevel(deviceID string) (*DeviceLevel, error) {
	l := &DeviceLevel{newMeterThread()}
	if err := l.start(func(ready chan<- error) { l.run(deviceID, ready) }); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *DeviceLevel) run(deviceID string, ready chan<- error) {
	enum, err := wincom.NewEnumerator()
	if err != nil {
		ready <- err
		return
	}
	defer enum.Release()
	dev, err := wincom.Find(enum, wincom.Render, deviceID)
	if err != nil {
		ready <- fmt.Errorf("finding device: %w", err)
		return
	}
	defer dev.Release()
	var meter *wca.IAudioMeterInformation
	if err := wincom.Activate(dev, wca.IID_IAudioMeterInformation, unsafe.Pointer(&meter)); err != nil {
		ready <- fmt.Errorf("activate meter: %w", describeHRESULT(err))
		return
	}
	defer meter.Release()
	ready <- nil

	t := time.NewTicker(meterPoll)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
		}
		var peak float32
		if err := meter.GetPeakValue(&peak); err != nil {
			l.fail(fmt.Errorf("meter: %w", describeHRESULT(err)))
			return
		}
		l.peak.raise(peak)
	}
}
