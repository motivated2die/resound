package audio

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
	"golang.org/x/sys/windows"
)

const (
	waveFormatPCM        = 0x0001
	waveFormatIEEEFloat  = 0x0003
	waveFormatExtensible = 0xFFFE
)

var (
	modAvrt                             = windows.NewLazySystemDLL("avrt.dll")
	procAvSetMmThreadCharacteristicsW   = modAvrt.NewProc("AvSetMmThreadCharacteristicsW")
	procAvRevertMmThreadCharacteristics = modAvrt.NewProc("AvRevertMmThreadCharacteristics")
)

// sampleFormat describes the shared-mode mix format of a device.
type sampleFormat struct {
	Rate       int
	Channels   int
	Bits       int // container bits per sample
	Float      bool
	BlockAlign int
}

func (f sampleFormat) String() string {
	kind := "int"
	if f.Float {
		kind = "float"
	}
	return fmt.Sprintf("%d Hz · %dch · %d-bit %s", f.Rate, f.Channels, f.Bits, kind)
}

// parseFormat reads a WAVEFORMATEX(TENSIBLE) returned by GetMixFormat. The C
// struct is packed: WAVEFORMATEX is 18 bytes and, for the extensible form,
// the SubFormat GUID starts at byte 24 (Data1 identifies PCM vs float).
func parseFormat(wfx *wca.WAVEFORMATEX) (sampleFormat, error) {
	f := sampleFormat{
		Rate:       int(wfx.NSamplesPerSec),
		Channels:   int(wfx.NChannels),
		Bits:       int(wfx.WBitsPerSample),
		BlockAlign: int(wfx.NBlockAlign),
	}
	tag := wfx.WFormatTag
	if tag == waveFormatExtensible && wfx.CbSize >= 22 {
		sub := *(*uint32)(unsafe.Add(unsafe.Pointer(wfx), 24))
		tag = uint16(sub)
	}
	switch {
	case tag == waveFormatIEEEFloat && f.Bits == 32:
		f.Float = true
	case tag == waveFormatPCM && (f.Bits == 16 || f.Bits == 24 || f.Bits == 32):
	default:
		return f, fmt.Errorf("%w (tag %#x, %d bits)", errUnsupportedFormat, tag, f.Bits)
	}
	if f.Channels < 1 || f.BlockAlign != f.Channels*f.Bits/8 {
		return f, fmt.Errorf("%w: unexpected layout (%d ch, block %d)", errUnsupportedFormat, f.Channels, f.BlockAlign)
	}
	return f, nil
}

// boostThreadPriority registers the current thread with MMCSS ("Pro Audio").
// It returns a function that reverts it. Failure is harmless.
func boostThreadPriority() func() {
	if procAvSetMmThreadCharacteristicsW.Find() != nil {
		return func() {}
	}
	name, _ := windows.UTF16PtrFromString("Pro Audio")
	var taskIndex uint32
	h, _, _ := procAvSetMmThreadCharacteristicsW.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&taskIndex)))
	if h == 0 {
		return func() {}
	}
	return func() { procAvRevertMmThreadCharacteristics.Call(h) }
}

// describeHRESULT replaces a COM error with one named by its HRESULT (see
// hresultError): go-ole's own text for WASAPI's codes is only
// FormatMessage failing. Errors described already pass unchanged.
func describeHRESULT(err error) error {
	var he *hresultError
	if errors.As(err, &he) {
		return err
	}
	var oleErr *ole.OleError
	if !errors.As(err, &oleErr) {
		return err
	}
	return &hresultError{code: uint32(oleErr.Code()), err: err}
}
