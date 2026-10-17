// Package wincom holds the shared Core Audio COM plumbing: COM thread setup,
// endpoint lookup and the few calls go-wca gets wrong or leaves unimplemented.
package wincom

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// ClsctxAll is CLSCTX_INPROC_SERVER|INPROC_HANDLER|LOCAL_SERVER|REMOTE_SERVER.
const ClsctxAll = 0x17

// Data-flow directions of an audio endpoint.
const (
	Render  = wca.ERender  // outputs (speakers, "CABLE Input")
	Capture = wca.ECapture // inputs (microphones, "CABLE Output")
)

// IID_IAudioEndpointVolume identifies IAudioEndpointVolume.
var IID_IAudioEndpointVolume = ole.NewGUID("{5CDF2C82-841E-4546-9722-0CF74078229A}")

// Init initialises COM (multithreaded) on the current OS thread, which the
// caller must have locked. S_FALSE (already initialised) counts as success
// and needs the same CoUninitialize.
func Init() error {
	err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	var oleErr *ole.OleError
	if err != nil && errors.As(err, &oleErr) && oleErr.Code() == 1 { // S_FALSE
		return nil
	}
	return err
}

// Run executes fn on a dedicated, COM-initialised OS thread and waits for it,
// keeping COM apartments away from the UI thread.
func Run(fn func() error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := Init(); err != nil {
			done <- fmt.Errorf("CoInitializeEx: %w", err)
			return
		}
		defer ole.CoUninitialize()
		done <- fn()
	}()
	return <-done
}

// NewEnumerator creates an IMMDeviceEnumerator. Release it when done.
func NewEnumerator() (*wca.IMMDeviceEnumerator, error) {
	var e *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, ClsctxAll, wca.IID_IMMDeviceEnumerator, &e); err != nil {
		return nil, fmt.Errorf("creating device enumerator: %w", err)
	}
	return e, nil
}

// Endpoint is one active device as returned by Enumerate.
type Endpoint struct {
	ID      string
	Name    string
	Default bool
}

// Enumerate lists the active endpoints of one direction. It must run on a
// COM thread (inside Run).
func Enumerate(e *wca.IMMDeviceEnumerator, flow uint32) ([]Endpoint, error) {
	defaultID := ""
	var def *wca.IMMDevice
	if err := e.GetDefaultAudioEndpoint(flow, wca.EConsole, &def); err == nil {
		_ = def.GetId(&defaultID)
		def.Release()
	}
	var out []Endpoint
	err := forEach(e, flow, func(dev *wca.IMMDevice, id string) bool {
		out = append(out, Endpoint{ID: id, Name: FriendlyName(dev), Default: id == defaultID})
		return true
	})
	return out, err
}

// ErrNotFound means no active endpoint has the ID: the device is unplugged,
// disabled or removed.
var ErrNotFound = errors.New("device not found or not active")

// Find returns the active endpoint with the given ID, or the default console
// endpoint of that direction when id is "". The caller releases it.
// (go-wca's IMMDeviceEnumerator.GetDevice is an unimplemented stub.)
func Find(e *wca.IMMDeviceEnumerator, flow uint32, id string) (*wca.IMMDevice, error) {
	if id == "" {
		var dev *wca.IMMDevice
		if err := e.GetDefaultAudioEndpoint(flow, wca.EConsole, &dev); err != nil {
			return nil, fmt.Errorf("no default device: %w", err)
		}
		return dev, nil
	}
	var found *wca.IMMDevice
	err := forEach(e, flow, func(dev *wca.IMMDevice, devID string) bool {
		if devID == id {
			dev.AddRef() // forEach releases its reference
			found = dev
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

// forEach visits every active endpoint; fn returns false to stop.
func forEach(e *wca.IMMDeviceEnumerator, flow uint32, fn func(dev *wca.IMMDevice, id string) bool) error {
	var col *wca.IMMDeviceCollection
	if err := e.EnumAudioEndpoints(flow, wca.DEVICE_STATE_ACTIVE, &col); err != nil {
		return fmt.Errorf("enumerating devices: %w", err)
	}
	defer col.Release()
	var count uint32
	if err := col.GetCount(&count); err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		var dev *wca.IMMDevice
		if err := col.Item(i, &dev); err != nil {
			continue
		}
		var id string
		cont := true
		if err := dev.GetId(&id); err == nil {
			cont = fn(dev, id)
		}
		dev.Release()
		if !cont {
			break
		}
	}
	return nil
}

// FriendlyName reads PKEY_Device_FriendlyName.
func FriendlyName(dev *wca.IMMDevice) string {
	var ps *wca.IPropertyStore
	if err := dev.OpenPropertyStore(wca.STGM_READ, &ps); err != nil {
		return "(unknown device)"
	}
	defer ps.Release()
	var pv wca.PROPVARIANT
	if err := ps.GetValue(&wca.PKEY_Device_FriendlyName, &pv); err != nil || pv.VT != ole.VT_LPWSTR {
		return "(unnamed device)"
	}
	return pv.String() // also frees the string
}

// Activate calls IMMDevice::Activate and stores the interface pointer in
// out (a pointer to a pointer of the wanted go-wca type). go-wca's wrapper
// passes a pointer where the CLSCTX value belongs, so the vtable is called
// directly.
func Activate(dev *wca.IMMDevice, iid *ole.GUID, out unsafe.Pointer) error {
	hr, _, _ := syscall.SyscallN(dev.VTable().Activate,
		uintptr(unsafe.Pointer(dev)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(ClsctxAll),
		0,
		uintptr(out))
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

// IsAccessDenied reports whether err is E_ACCESSDENIED.
func IsAccessDenied(err error) bool {
	var oleErr *ole.OleError
	return errors.As(err, &oleErr) && uint32(oleErr.Code()) == 0x80070005
}

// GetMute reads IAudioEndpointVolume's mute state. go-wca's GetMute passes a
// 1-byte Go bool where Windows writes a 4-byte BOOL, corrupting the stack,
// so the vtable is called directly with a proper 32-bit value.
func GetMute(v *wca.IAudioEndpointVolume) (bool, error) {
	var muted int32
	hr, _, _ := syscall.SyscallN(v.VTable().GetMute,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(&muted)))
	if hr != 0 {
		return false, ole.NewError(hr)
	}
	return muted != 0, nil
}

// Device roles of a default endpoint (ERole).
const (
	RoleConsole        = wca.EConsole
	RoleMultimedia     = wca.EMultimedia
	RoleCommunications = wca.ECommunications
)

// DefaultID returns the default endpoint's ID for a direction and role, or
// "" when there is none. It must run on a COM thread (inside Run).
func DefaultID(e *wca.IMMDeviceEnumerator, flow, role uint32) string {
	var dev *wca.IMMDevice
	if err := e.GetDefaultAudioEndpoint(flow, role, &dev); err != nil {
		return ""
	}
	defer dev.Release()
	var id string
	_ = dev.GetId(&id)
	return id
}

// IPolicyConfig is undocumented but stable since Windows 7; it is what the
// Sound control panel uses to change default devices (no admin needed).
var (
	clsidPolicyConfigClient = ole.NewGUID("{870AF99C-171D-4F9E-AF0D-E63DF40C2BC9}")
	iidPolicyConfig         = ole.NewGUID("{F8679F50-850A-41CF-9C72-430F290290C8}")
)

// policyConfigSetDefaultEndpoint is SetDefaultEndpoint's vtable slot: the
// three IUnknown methods, then ten IPolicyConfig methods before it.
const policyConfigSetDefaultEndpoint = 13

// SetDefaultEndpoint makes the endpoint the default for role. It must run on
// a COM thread (inside Run).
func SetDefaultEndpoint(id string, role uint32) error {
	pc, err := ole.CreateInstance(clsidPolicyConfigClient, iidPolicyConfig)
	if err != nil {
		return fmt.Errorf("creating PolicyConfig: %w", err)
	}
	defer pc.Release()
	wid, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	vtbl := (*[policyConfigSetDefaultEndpoint + 1]uintptr)(unsafe.Pointer(pc.RawVTable))
	hr, _, _ := syscall.SyscallN(vtbl[policyConfigSetDefaultEndpoint],
		uintptr(unsafe.Pointer(pc)),
		uintptr(unsafe.Pointer(wid)),
		uintptr(role))
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}
