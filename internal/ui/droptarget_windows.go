package ui

// Drag and drop onto the panel through OLE (RegisterDragDrop), so the panel
// accepts web links dragged from a browser as well as files and folders
// from Explorer. WM_DROPFILES (DragAcceptFiles) only ever carries files.
//
// The IDropTarget is a static COM object: a vtable of Go callbacks. OLE
// calls it on the UI thread from its message loop while something is
// dragged over the panel, so it may touch the popup directly.

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DropData is what was dropped on the panel: files and folders from
// Explorer, or web links (http/https) from a browser.
type DropData struct {
	Paths []string
	Links []string
}

// dropper is implemented by rows that are drop fields of their own. A drop
// anywhere on such a row goes to its handler instead of Popup.OnDrop.
type dropper interface {
	dropHandler() func(DropData)
}

var (
	ole32                        = windows.NewLazySystemDLL("ole32.dll")
	procOleInitialize            = ole32.NewProc("OleInitialize")
	procRegisterDragDrop         = ole32.NewProc("RegisterDragDrop")
	procReleaseStgMedium         = ole32.NewProc("ReleaseStgMedium")
	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	procScreenToClient           = user32.NewProc("ScreenToClient")
	procGlobalLock               = kernel32.NewProc("GlobalLock")
	procGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
	procGlobalSize               = kernel32.NewProc("GlobalSize")
)

const (
	cfUnicodeText   = 13
	cfHDrop         = 15
	tymedHGlobal    = 1
	dvaspectContent = 1

	dropEffectNone = 0
	dropEffectCopy = 1
	dropEffectLink = 4

	sOK          = 0
	eNoInterface = 0x80004002
)

// formatEtc is FORMATETC; stgMedium is STGMEDIUM (64-bit layouts).
type formatEtc struct {
	cfFormat uint16
	ptd      uintptr
	aspect   uint32
	lindex   int32
	tymed    uint32
}

type stgMedium struct {
	tymed   uint32
	data    uintptr // HGLOBAL for TYMED_HGLOBAL
	release uintptr
}

type dropTargetVtbl struct {
	queryInterface, addRef, release, dragEnter, dragOver, dragLeave, drop uintptr
}

// dropTargetObject is the COM object: its first word points to the vtable.
// Both live in package variables, so they never move and are never freed.
type dropTargetObject struct{ vtbl *dropTargetVtbl }

var (
	dropVtbl   dropTargetVtbl
	dropObject = dropTargetObject{&dropVtbl}

	// Link formats: browsers put a dragged link in several; the first that
	// is there wins. Firefox's text/x-moz-url is "url\ntitle".
	cfURLW, cfURL, cfMozURL uint16
)

var (
	iidIUnknown    = windows.GUID{Data1: 0x00000000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDropTarget = windows.GUID{Data1: 0x00000122, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// registerDropTarget makes p an OLE drop target. It returns false when OLE
// is unavailable; the popup then falls back to DragAcceptFiles (files only).
// UI thread, once.
func registerDropTarget(p *Popup) bool {
	if r, _, _ := procOleInitialize.Call(0); r != sOK && r != 1 /* S_FALSE: already initialized */ {
		return false
	}
	dropVtbl = dropTargetVtbl{
		queryInterface: windows.NewCallback(dtQueryInterface),
		addRef:         windows.NewCallback(dtAddRef),
		release:        windows.NewCallback(dtAddRef),
		dragEnter:      windows.NewCallback(dtDragEnter),
		dragOver:       windows.NewCallback(dtDragOver),
		dragLeave:      windows.NewCallback(dtDragLeave),
		drop:           windows.NewCallback(dtDrop),
	}
	cfURLW = registerFormat("UniformResourceLocatorW")
	cfURL = registerFormat("UniformResourceLocator")
	cfMozURL = registerFormat("text/x-moz-url")
	r, _, _ := procRegisterDragDrop.Call(p.hwnd, uintptr(unsafe.Pointer(&dropObject)))
	return r == sOK
}

func registerFormat(name string) uint16 {
	r, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(utf16Ptr(name))))
	return uint16(r)
}

func dtQueryInterface(this uintptr, riid *windows.GUID, ppv *uintptr) uintptr {
	if *riid == iidIUnknown || *riid == iidIDropTarget {
		*ppv = this
		return sOK
	}
	*ppv = 0
	return eNoInterface
}

func dtAddRef(uintptr) uintptr { return 1 } // static object: counting is moot

// The POINTL argument arrives by value in one register: x low, y high.
func dropPoint(pt uintptr) point { return point{X: int32(uint32(pt)), Y: int32(uint32(pt >> 32))} }

func dtDragEnter(_ uintptr, data unsafe.Pointer, _ uintptr, pt uintptr, effect *uint32) uintptr {
	p := panelPopup
	p.dragData = readDropData(data)
	*effect = p.dragOver(dropPoint(pt), *effect)
	return sOK
}

func dtDragOver(_ uintptr, _ uintptr, pt uintptr, effect *uint32) uintptr {
	*effect = panelPopup.dragOver(dropPoint(pt), *effect)
	return sOK
}

func dtDragLeave(uintptr) uintptr {
	panelPopup.endDragOver()
	return sOK
}

func dtDrop(_ uintptr, data unsafe.Pointer, _ uintptr, pt uintptr, effect *uint32) uintptr {
	p := panelPopup
	d := readDropData(data)
	*effect = p.dragOver(dropPoint(pt), *effect)
	handler := p.dropTargetAt(dropPoint(pt))
	p.endDragOver()
	if *effect == dropEffectNone || handler == nil {
		return sOK
	}
	// Let the user type right away (e.g. a name for a downloaded sound);
	// Windows may refuse while the browser keeps the focus.
	procSetForegroundWindow.Call(p.hwnd)
	handler(d) // may call SetRows
	return sOK
}

// dragOver updates the highlighted drop field for screen point pt and
// returns the effect to show: copy (or link) where a drop is accepted.
func (p *Popup) dragOver(pt point, allowed uint32) uint32 {
	row := -1
	accept := false
	if d := p.dragData; len(d.Paths) > 0 || len(d.Links) > 0 {
		c := p.clientPoint(pt)
		if i := p.rowAt(c.Y); i >= 0 {
			if dr, ok := p.slots[i].row.(dropper); ok && dr.dropHandler() != nil {
				row, accept = i, true
			}
		}
		if !accept && p.drops && p.OnDrop != nil {
			accept = true
		}
	}
	if row != p.dropRow {
		p.dropRow = row
		procInvalidateRect.Call(p.hwnd, 0, 0)
	}
	switch {
	case !accept:
		return dropEffectNone
	case allowed&dropEffectCopy != 0:
		return dropEffectCopy
	case allowed&dropEffectLink != 0:
		return dropEffectLink
	}
	return dropEffectNone
}

// dropTargetAt returns the handler for a drop at screen point pt: the drop
// field there, or OnDrop while the page accepts drops.
func (p *Popup) dropTargetAt(pt point) func(DropData) {
	if i := p.rowAt(p.clientPoint(pt).Y); i >= 0 {
		if dr, ok := p.slots[i].row.(dropper); ok && dr.dropHandler() != nil {
			return dr.dropHandler()
		}
	}
	if p.drops {
		return p.OnDrop
	}
	return nil
}

func (p *Popup) endDragOver() {
	p.dragData = DropData{}
	if p.dropRow >= 0 {
		p.dropRow = -1
		procInvalidateRect.Call(p.hwnd, 0, 0)
	}
}

func (p *Popup) clientPoint(pt point) point {
	procScreenToClient.Call(p.hwnd, uintptr(unsafe.Pointer(&pt)))
	return pt
}

// rowAt returns the slot at window y, or -1; a row scrolled under the dock
// isn't there.
func (p *Popup) rowAt(y int32) int {
	for i := range p.slots {
		if r := p.rowRect(i); y >= r.Top && y < r.Bottom && p.reaches(i, y) {
			return i
		}
	}
	return -1
}

// --- reading the dragged data ---------------------------------------------------

// readDropData takes the files or the web link out of an IDataObject.
func readDropData(obj unsafe.Pointer) DropData {
	var d DropData
	if obj == nil {
		return d
	}
	if hg, release, ok := getData(obj, cfHDrop); ok {
		d.Paths = dragFiles(hg)
		release()
		if len(d.Paths) > 0 {
			return d
		}
	}
	for _, cf := range []uint16{cfURLW, cfMozURL, cfURL, cfUnicodeText} {
		if cf == 0 {
			continue
		}
		hg, release, ok := getData(obj, cf)
		if !ok {
			continue
		}
		text := globalText(hg, cf == cfURL)
		release()
		text, _, _ = strings.Cut(strings.TrimSpace(text), "\n") // moz-url: the title follows
		text = strings.TrimSpace(text)
		if lower := strings.ToLower(text); strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
			d.Links = []string{text}
			return d
		}
	}
	return d
}

// getData calls IDataObject::GetData (vtable slot 3) for an HGLOBAL.
func getData(obj unsafe.Pointer, cf uint16) (hg uintptr, release func(), ok bool) {
	vtbl := *(**[12]uintptr)(obj)
	fe := formatEtc{cfFormat: cf, aspect: dvaspectContent, lindex: -1, tymed: tymedHGlobal}
	var med stgMedium
	r, _, _ := syscall.SyscallN(vtbl[3], uintptr(obj), uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med)))
	if r != sOK || med.tymed != tymedHGlobal || med.data == 0 {
		if r == sOK {
			procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&med)))
		}
		return 0, nil, false
	}
	return med.data, func() { procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&med))) }, true
}

// dragFiles lists the paths in an HDROP without freeing it (OLE owns it).
func dragFiles(hdrop uintptr) []string {
	n, _, _ := procDragQueryFileW.Call(hdrop, 0xFFFFFFFF, 0, 0)
	var out []string
	for i := uintptr(0); i < n; i++ {
		size, _, _ := procDragQueryFileW.Call(hdrop, i, 0, 0)
		buf := make([]uint16, size+1)
		procDragQueryFileW.Call(hdrop, i, uintptr(unsafe.Pointer(&buf[0])), size+1)
		out = append(out, windows.UTF16ToString(buf))
	}
	return out
}

// globalText reads a NUL-terminated string from an HGLOBAL: UTF-16, or the
// ANSI code page when ansi is set (CF "UniformResourceLocator").
func globalText(hg uintptr, ansi bool) string {
	ptr, _, _ := procGlobalLock.Call(hg)
	if ptr == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(hg)
	size, _, _ := procGlobalSize.Call(hg)
	if size == 0 || size > 1<<20 {
		return ""
	}
	if ansi {
		b := unsafe.Slice((*byte)(unsafe.Add(nil, ptr)), size)
		if i := strings.IndexByte(string(b), 0); i >= 0 {
			b = b[:i]
		}
		return string(b) // URLs are ASCII; anything else fails the link check
	}
	u := unsafe.Slice((*uint16)(unsafe.Add(nil, ptr)), size/2)
	return windows.UTF16ToString(u)
}
