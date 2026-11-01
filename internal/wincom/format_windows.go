package wincom

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
)

// IPolicyConfig's vtable slots for an endpoint's format in the Sound control
// panel's Advanced tab: the three IUnknown methods come first. It needs no
// admin rights, like that tab.
const (
	policyConfigGetMixFormat    = 3
	policyConfigGetDeviceFormat = 4
	policyConfigSetDeviceFormat = 6
)

// The parts of a WAVEFORMATEX that matter here.
const (
	waveFormatHeader     = 18
	waveFormatExtensible = 0xFFFE
	waveFormatMaxExtra   = 64 // WAVEFORMATEXTENSIBLE needs 22
)

// formatRate reads a WAVEFORMATEX's sample rate.
func formatRate(f []byte) int { return int(binary.LittleEndian.Uint32(f[4:])) }

// formatWithRate returns a copy of a WAVEFORMATEX at another sample rate,
// its bit depth and channels unchanged.
func formatWithRate(f []byte, rate int) []byte {
	out := append([]byte(nil), f...)
	binary.LittleEndian.PutUint32(out[4:], uint32(rate))
	blockAlign := binary.LittleEndian.Uint16(f[12:])
	binary.LittleEndian.PutUint32(out[8:], uint32(rate)*uint32(blockAlign))
	return out
}

// copyFormat copies a WAVEFORMATEX that Windows allocated, and frees it.
func copyFormat(p *byte) ([]byte, error) {
	if p == nil {
		return nil, errors.New("no format")
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(p)))
	head := unsafe.Slice(p, waveFormatHeader)
	size := waveFormatHeader
	if binary.LittleEndian.Uint16(head) == waveFormatExtensible {
		extra := int(binary.LittleEndian.Uint16(head[16:]))
		if extra > waveFormatMaxExtra {
			return nil, fmt.Errorf("unexpected format size %d", extra)
		}
		size += extra
	}
	return append([]byte(nil), unsafe.Slice(p, size)...), nil
}

// readFormat reads an endpoint's device format, or its mix format.
func readFormat(pc *ole.IUnknown, id *uint16, mix bool) ([]byte, error) {
	var p *byte
	var hr uintptr
	if mix {
		vtbl := (*[policyConfigGetMixFormat + 1]uintptr)(unsafe.Pointer(pc.RawVTable))
		hr, _, _ = syscall.SyscallN(vtbl[policyConfigGetMixFormat],
			uintptr(unsafe.Pointer(pc)), uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(&p)))
	} else {
		vtbl := (*[policyConfigGetDeviceFormat + 1]uintptr)(unsafe.Pointer(pc.RawVTable))
		hr, _, _ = syscall.SyscallN(vtbl[policyConfigGetDeviceFormat],
			uintptr(unsafe.Pointer(pc)), uintptr(unsafe.Pointer(id)), 0, uintptr(unsafe.Pointer(&p)))
	}
	if hr != 0 {
		return nil, ole.NewError(hr)
	}
	return copyFormat(p)
}

func newPolicyConfig(id string) (*ole.IUnknown, *uint16, error) {
	wid, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		return nil, nil, err
	}
	pc, err := ole.CreateInstance(clsidPolicyConfigClient, iidPolicyConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("creating PolicyConfig: %w", err)
	}
	return pc, wid, nil
}

// DeviceRate returns the sample rate set for an endpoint in Windows. It must
// run on a COM thread (inside Run).
func DeviceRate(id string) (int, error) {
	pc, wid, err := newPolicyConfig(id)
	if err != nil {
		return 0, err
	}
	defer pc.Release()
	f, err := readFormat(pc, wid, false)
	if err != nil {
		return 0, err
	}
	return formatRate(f), nil
}

// SetDeviceRate sets an endpoint's sample rate, keeping its bit depth and
// channels, and checks that it took. Streams open on the endpoint fail and
// have to be opened again. It must run on a COM thread (inside Run).
func SetDeviceRate(id string, rate int) error {
	pc, wid, err := newPolicyConfig(id)
	if err != nil {
		return err
	}
	defer pc.Release()
	device, err := readFormat(pc, wid, false)
	if err != nil {
		return fmt.Errorf("reading the device format: %w", err)
	}
	if formatRate(device) == rate {
		return nil
	}
	mix, err := readFormat(pc, wid, true)
	if err != nil {
		return fmt.Errorf("reading the mix format: %w", err)
	}
	device, mix = formatWithRate(device, rate), formatWithRate(mix, rate)
	vtbl := (*[policyConfigSetDeviceFormat + 1]uintptr)(unsafe.Pointer(pc.RawVTable))
	hr, _, _ := syscall.SyscallN(vtbl[policyConfigSetDeviceFormat],
		uintptr(unsafe.Pointer(pc)), uintptr(unsafe.Pointer(wid)),
		uintptr(unsafe.Pointer(&device[0])), uintptr(unsafe.Pointer(&mix[0])))
	if hr != 0 {
		return ole.NewError(hr)
	}
	got, err := readFormat(pc, wid, false)
	if err != nil {
		return fmt.Errorf("checking the device format: %w", err)
	}
	if formatRate(got) != rate {
		return fmt.Errorf("the rate is still %d Hz", formatRate(got))
	}
	return nil
}
