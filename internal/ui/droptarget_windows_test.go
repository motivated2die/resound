package ui

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSHCreateDataObject = shell32.NewProc("SHCreateDataObject")
	procGlobalAlloc        = kernel32.NewProc("GlobalAlloc")
	iidIDataObject         = windows.GUID{Data1: 0x0000010e, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// newDataObject returns an empty shell data object, as a browser or
// Explorer would hand one to a drop target.
func newDataObject(t *testing.T) unsafe.Pointer {
	t.Helper()
	var obj unsafe.Pointer
	r, _, _ := procSHCreateDataObject.Call(0, 0, 0, 0, uintptr(unsafe.Pointer(&iidIDataObject)), uintptr(unsafe.Pointer(&obj)))
	if r != sOK || obj == nil {
		t.Skipf("SHCreateDataObject: %#x", r)
	}
	return obj
}

// setText stores a NUL-terminated UTF-16 string under format cf
// (IDataObject::SetData, vtable slot 7; the object takes the memory).
func setText(t *testing.T, obj unsafe.Pointer, cf uint16, s string) {
	t.Helper()
	u, _ := windows.UTF16FromString(s)
	hg, _, _ := procGlobalAlloc.Call(2 /* GMEM_MOVEABLE */, uintptr(len(u)*2))
	ptr, _, _ := procGlobalLock.Call(hg)
	copy(unsafe.Slice((*uint16)(unsafe.Add(nil, ptr)), len(u)), u)
	procGlobalUnlock.Call(hg)
	fe := formatEtc{cfFormat: cf, aspect: dvaspectContent, lindex: -1, tymed: tymedHGlobal}
	med := stgMedium{tymed: tymedHGlobal, data: hg}
	vtbl := *(**[12]uintptr)(obj)
	if r, _, _ := syscall.SyscallN(vtbl[7], uintptr(obj), uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med)), 1); r != sOK {
		t.Fatalf("SetData: %#x", r)
	}
}

func release(obj unsafe.Pointer) {
	vtbl := *(**[12]uintptr)(obj)
	syscall.SyscallN(vtbl[2], uintptr(obj))
}

// Links arrive in the formats browsers use; readDropData takes them out
// through real OLE calls, which also checks the FORMATETC/STGMEDIUM layouts.
func TestReadDropDataLinks(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if r, _, _ := procOleInitialize.Call(0); r != sOK && r != 1 {
		t.Skipf("OleInitialize: %#x", r)
	}
	cfURLW = registerFormat("UniformResourceLocatorW")
	cfMozURL = registerFormat("text/x-moz-url")
	const link = "https://www.myinstants.com/en/instant/vine-boom-sound-70972/"

	for _, tc := range []struct {
		name string
		cf   uint16
		text string
		want string
	}{
		{"Chromium and Edge", cfURLW, link, link},
		{"Firefox", cfMozURL, link + "\nVINE BOOM SOUND", link},
		{"plain text", cfUnicodeText, "  " + link + "\r\n", link},
		{"text that isn't a link", cfUnicodeText, "VINE BOOM SOUND", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := newDataObject(t)
			defer release(obj)
			setText(t, obj, tc.cf, tc.text)
			d := readDropData(obj)
			if len(d.Paths) != 0 {
				t.Errorf("paths = %v", d.Paths)
			}
			got := ""
			if len(d.Links) > 0 {
				got = d.Links[0]
			}
			if got != tc.want {
				t.Errorf("links = %v, want %q", d.Links, tc.want)
			}
		})
	}
}
