package routing

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
	"golang.org/x/sys/windows"
)

// The "Listen" tab of a recording device in mmsys.cpl stores its state in two
// undocumented endpoint properties (found by EinfachEinAlex for Soundux):
//
//	{24dbb0fc-9311-4b3d-9cf0-18ff155639d4},1  VT_BOOL   "Listen to this device"
//	{24dbb0fc-9311-4b3d-9cf0-18ff155639d4},0  VT_LPWSTR "Playback through this device"
//
// The second holds a render endpoint ID ("{0.0.0.00000000}.{guid}"); empty
// means "Default Playback Device". Writing either needs administrator rights.
var (
	pkeyListenTarget = wca.DefinePropertyKey(0x24dbb0fc, 0x9311, 0x4b3d, 0x9c, 0xf0, 0x18, 0xff, 0x15, 0x56, 0x39, 0xd4, 0)
	pkeyListen       = wca.DefinePropertyKey(0x24dbb0fc, 0x9311, 0x4b3d, 0x9c, 0xf0, 0x18, 0xff, 0x15, 0x56, 0x39, 0xd4, 1)
)

const variantTrue = 0xFFFF // VARIANT_TRUE as stored in the low 16 bits

// readBool returns a VT_BOOL property; missing values read as false.
func readBool(ps *wca.IPropertyStore, key wca.PROPERTYKEY) bool {
	var pv wca.PROPVARIANT
	if err := ps.GetValue(&key, &pv); err != nil || pv.VT != ole.VT_BOOL {
		return false
	}
	return pv.Val&0xFFFF != 0
}

// readString returns a VT_LPWSTR property; missing values read as "".
func readString(ps *wca.IPropertyStore, key wca.PROPERTYKEY) string {
	var pv wca.PROPVARIANT
	if err := ps.GetValue(&key, &pv); err != nil || pv.VT != ole.VT_LPWSTR || pv.Val == 0 {
		return ""
	}
	return pv.String() // also frees the string
}

// setValue calls IPropertyStore::SetValue (a stub in go-wca).
func setValue(ps *wca.IPropertyStore, key wca.PROPERTYKEY, pv *wca.PROPVARIANT) error {
	hr, _, _ := syscall.SyscallN(ps.VTable().SetValue,
		uintptr(unsafe.Pointer(ps)),
		uintptr(unsafe.Pointer(&key)),
		uintptr(unsafe.Pointer(pv)))
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

// commit calls IPropertyStore::Commit (a stub in go-wca). The endpoint store
// already persists on SetValue; Commit is called for good measure.
func commit(ps *wca.IPropertyStore) error {
	hr, _, _ := syscall.SyscallN(ps.VTable().Commit, uintptr(unsafe.Pointer(ps)))
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

func writeBool(ps *wca.IPropertyStore, key wca.PROPERTYKEY, v bool) error {
	pv := wca.PROPVARIANT{}
	pv.VT = ole.VT_BOOL
	if v {
		pv.Val = variantTrue
	}
	return setValue(ps, key, &pv)
}

func writeString(ps *wca.IPropertyStore, key wca.PROPERTYKEY, s string) error {
	u, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return fmt.Errorf("invalid value %q: %w", s, err)
	}
	pv := wca.PROPVARIANT{}
	pv.VT = ole.VT_LPWSTR
	pv.Val = int64(uintptr(unsafe.Pointer(u)))
	err = setValue(ps, key, &pv) // SetValue copies the string
	runtime.KeepAlive(u)
	return err
}
