// Package ui implements the tray icon and the custom-drawn popup panel with
// plain Win32 + GDI calls (no cgo, no UI framework).
package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	shcore   = windows.NewLazySystemDLL("shcore.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	comdlg32 = windows.NewLazySystemDLL("comdlg32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	procFindWindowW                   = user32.NewProc("FindWindowW")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procMonitorFromPoint              = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procInvalidateRect                = user32.NewProc("InvalidateRect")
	procBeginPaint                    = user32.NewProc("BeginPaint")
	procEndPaint                      = user32.NewProc("EndPaint")
	procFillRect                      = user32.NewProc("FillRect")
	procDrawTextW                     = user32.NewProc("DrawTextW")
	procTrackMouseEvent               = user32.NewProc("TrackMouseEvent")
	procLoadImageW                    = user32.NewProc("LoadImageW")
	procLoadIconW                     = user32.NewProc("LoadIconW")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procCreateFontIndirectW           = gdi32.NewProc("CreateFontIndirectW")
	procCreateSolidBrush              = gdi32.NewProc("CreateSolidBrush")
	procCreatePen                     = gdi32.NewProc("CreatePen")
	procSelectObject                  = gdi32.NewProc("SelectObject")
	procDeleteObject                  = gdi32.NewProc("DeleteObject")
	procCreateCompatibleDC            = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap        = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteDC                      = gdi32.NewProc("DeleteDC")
	procBitBlt                        = gdi32.NewProc("BitBlt")
	procSetTextColor                  = gdi32.NewProc("SetTextColor")
	procSetBkMode                     = gdi32.NewProc("SetBkMode")
	procRoundRect                     = gdi32.NewProc("RoundRect")
	procEllipse                       = gdi32.NewProc("Ellipse")
	procGetStockObject                = gdi32.NewProc("GetStockObject")
	procShellNotifyIconW              = shell32.NewProc("Shell_NotifyIconW")
	procGetDpiForMonitor              = shcore.NewProc("GetDpiForMonitor")
	procDwmSetWindowAttribute         = dwmapi.NewProc("DwmSetWindowAttribute")
	procGetOpenFileNameW              = comdlg32.NewProc("GetOpenFileNameW")
	procCommDlgExtendedError          = comdlg32.NewProc("CommDlgExtendedError")
	procGetModuleHandleW              = kernel32.NewProc("GetModuleHandleW")
)

// Window messages and styles.
const (
	wmDestroy                = 0x0002
	wmSize                   = 0x0005
	wmActivate               = 0x0006
	wmPaint                  = 0x000F
	wmClose                  = 0x0010
	wmEndSession             = 0x0016
	wmEraseBkgnd             = 0x0014
	wmMouseMove              = 0x0200
	wmLButtonDown            = 0x0201
	wmLButtonUp              = 0x0202
	wmRButtonUp              = 0x0205
	wmMouseWheel             = 0x020A
	wmMouseLeave             = 0x02A3
	wmCaptureChanged         = 0x0215
	wmKeyDown                = 0x0100
	wmKeyUp                  = 0x0101
	wmSysKeyDown             = 0x0104
	wmSysKeyUp               = 0x0105
	wmMButtonDown            = 0x0207
	wmMButtonUp              = 0x0208
	wmXButtonDown            = 0x020B
	wmXButtonUp              = 0x020C
	wmApp                    = 0x8000
	wmContextMenu            = 0x007B
	waInactive               = 0
	vkEscape                 = 0x1B
	vkBrowserBack            = 0xA6
	vkReturn                 = 0x0D
	vkUp                     = 0x26
	vkDown                   = 0x28
	vkHome                   = 0x24
	vkEnd                    = 0x23
	wmTimer                  = 0x0113
	wsPopup                  = 0x80000000
	wsExToolWindow           = 0x00000080
	wsExTopmost              = 0x00000008
	csDropShadow             = 0x00020000
	swHide                   = 0
	swShow                   = 5
	swpNoZOrder              = 0x0004
	swpNoActivate            = 0x0010
	swpShowWindow            = 0x0040
	hwndTopmost              = ^uintptr(0) // (HWND)-1
	tmeLeave                 = 0x00000002
	idcArrow                 = 32512
	idcHand                  = 32649
	idiApplication           = 32512
	imageIcon                = 1
	lrDefaultSize            = 0x0040
	lrShared                 = 0x8000
	monitorNearest           = 2
	mdtEffectiveDpi          = 0
	transparent              = 1
	nullPen                  = 8
	dpiAwarenessPerMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4

	dwmwaWindowCornerPreference = 33
	dwmwcpRound                 = 2
	dwmwaBorderColor            = 34
	dwmColorNone                = 0xFFFFFFFE // no border

	dtLeft        = 0x0000
	dtCenter      = 0x0001
	dtRight       = 0x0002
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtNoPrefix    = 0x0800
	dtEndEllipsis = 0x8000
)

// Shell_NotifyIcon.
const (
	nimAdd     = 0x0
	nimModify  = 0x1
	nimDelete  = 0x2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) w() int32 { return r.Right - r.Left }
func (r rect) h() int32 { return r.Bottom - r.Top }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	_       uint32 // lPrivate
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type paintStruct struct {
	Hdc         uintptr
	Erase       int32
	RcPaint     rect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type trackMouseEventStruct struct {
	Size      uint32
	Flags     uint32
	Track     uintptr
	HoverTime uint32
}

type logFont struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

type notifyIconData struct {
	Size             uint32
	Wnd              uintptr
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             uintptr
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUIDItem         windows.GUID
	BalloonIcon      uintptr
}

func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString("?")
	}
	return p
}

func loword(v uintptr) int32 { return int32(int16(v & 0xFFFF)) }
func hiword(v uintptr) int32 { return int32(int16((v >> 16) & 0xFFFF)) }

// rgb builds a COLORREF (0x00BBGGRR).
func rgb(r, g, b uint8) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

func moduleHandle() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

func registerClass(name string, style uint32, proc uintptr) error {
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		Style:     style,
		WndProc:   proc,
		Instance:  moduleHandle(),
		Cursor:    cursor,
		ClassName: utf16Ptr(name),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	return nil
}

func createWindow(exStyle, style uint32, class string) (uintptr, error) {
	return createOwnedWindow(exStyle, style, class, 0)
}

// createOwnedWindow creates a top-level window owned by owner: Windows
// always keeps it above its owner and raises them together.
func createOwnedWindow(exStyle, style uint32, class string, owner uintptr) (uintptr, error) {
	h, _, err := procCreateWindowExW.Call(uintptr(exStyle), uintptr(unsafe.Pointer(utf16Ptr(class))), 0,
		uintptr(style), 0, 0, 0, 0, owner, 0, moduleHandle(), 0)
	if h == 0 {
		return 0, err
	}
	return h, nil
}

func defWindowProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), w, l)
	return r
}

// InitDPIAwareness opts the process into per-monitor v2 DPI awareness so GDI
// text stays sharp on scaled displays. The manifest can do the same; this
// covers builds without resources.
func InitDPIAwareness() {
	if procSetProcessDpiAwarenessContext.Find() == nil {
		procSetProcessDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
	}
}

// windowBorder is the colour of the 1-pixel border around rounded windows:
// about what Windows 11's own translucent grey looks like on a dark desktop.
var windowBorder = rgb(0x38, 0x38, 0x38)

// roundCorners asks Windows 11 to round a window's corners, smoothly, as
// it does for its own windows, with border as the colour of the 1-pixel
// border it draws then (dwmColorNone: none, the window paints its own). It
// returns false where it can't (Windows 10).
//
// Windows' border is translucent by default, so whatever lies behind shows
// through it: the panel's sides went lighter where its tab is tucked under
// them. An opaque colour looks the same everywhere.
func roundCorners(hwnd uintptr, border uint32) bool {
	if procDwmSetWindowAttribute.Find() != nil {
		return false
	}
	pref := uint32(dwmwcpRound)
	hr, _, _ := procDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&pref)), 4)
	procDwmSetWindowAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&border)), 4)
	return hr == 0
}

// monitorAt returns the work area and DPI scale of the monitor at pt.
func monitorAt(pt point) (work rect, scale float64) {
	mon, _, _ := procMonitorFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&pt))), monitorNearest)
	mi := monitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	scale = 1
	if procGetDpiForMonitor.Find() == nil {
		var dx, dy uint32
		if r, _, _ := procGetDpiForMonitor.Call(mon, mdtEffectiveDpi, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx > 0 {
			scale = float64(dx) / 96
		}
	}
	return mi.Work, scale
}

func cursorPos() point {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}
