package ui

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openFileNameW mirrors OPENFILENAMEW. On 64-bit Windows the C struct uses
// natural alignment, which matches Go's layout.
type openFileNameW struct {
	StructSize    uint32
	Owner         uintptr
	Instance      uintptr
	Filter        *uint16
	CustomFilter  *uint16
	MaxCustFilter uint32
	FilterIndex   uint32
	File          *uint16
	MaxFile       uint32
	FileTitle     *uint16
	MaxFileTitle  uint32
	InitialDir    *uint16
	Title         *uint16
	Flags         uint32
	FileOffset    uint16
	FileExtension uint16
	DefExt        *uint16
	CustData      uintptr
	FnHook        uintptr
	TemplateName  *uint16
	Reserved      uintptr
	Reserved2     uint32
	FlagsEx       uint32
}

const (
	ofnFileMustExist   = 0x00001000
	ofnPathMustExist   = 0x00000800
	ofnNoChangeDir     = 0x00000008
	ofnExplorer        = 0x00080000
	ofnOverwritePrompt = 0x00000002
)

var procGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")

// FileFilter is one entry of the file type dropdown, e.g.
// {"Audio files", "*.mp3;*.wav"}.
type FileFilter struct{ Name, Pattern string }

// OpenFileDialog shows the standard "Open" dialog. ok is false when the user
// cancelled. Must run on the UI thread.
func OpenFileDialog(owner uintptr, title string, filters []FileFilter) (path string, ok bool, err error) {
	return fileDialog(procGetOpenFileNameW, owner, title, "", "", "", filters, ofnFileMustExist)
}

// SaveFileDialog shows the standard "Save as" dialog, starting in dir with
// name filled in. ext (without dot) is appended when the user types none;
// replacing an existing file asks first.
func SaveFileDialog(owner uintptr, title, dir, name, ext string, filters []FileFilter) (path string, ok bool, err error) {
	return fileDialog(procGetSaveFileNameW, owner, title, dir, name, ext, filters, ofnOverwritePrompt)
}

func fileDialog(proc *windows.LazyProc, owner uintptr, title, dir, name, ext string, filters []FileFilter, flags uint32) (string, bool, error) {
	var spec []uint16
	for _, f := range filters {
		n, _ := windows.UTF16FromString(f.Name)
		p, _ := windows.UTF16FromString(f.Pattern)
		spec = append(spec, n...) // includes the terminating 0
		spec = append(spec, p...)
	}
	spec = append(spec, 0) // double-null terminated list

	buf := make([]uint16, windows.MAX_LONG_PATH)
	if u, err := windows.UTF16FromString(name); err == nil && len(u) < len(buf) {
		copy(buf, u)
	}
	ofn := openFileNameW{
		Owner:   owner,
		Filter:  &spec[0],
		File:    &buf[0],
		MaxFile: uint32(len(buf)),
		Title:   utf16Ptr(title),
		Flags:   flags | ofnPathMustExist | ofnNoChangeDir | ofnExplorer,
	}
	if dir != "" {
		ofn.InitialDir = utf16Ptr(dir)
	}
	if ext != "" {
		ofn.DefExt = utf16Ptr(ext)
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return "", false, fmt.Errorf("file dialog error %#x", code)
		}
		return "", false, nil // cancelled
	}
	return windows.UTF16ToString(buf), true, nil
}

// ShowError shows a modal error message box.
func ShowError(owner uintptr, title, text string) {
	windows.MessageBox(windows.HWND(owner), utf16Ptr(text), utf16Ptr(title), windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}

// ShellOpen opens a file or folder with its default handler (Explorer for
// folders).
func ShellOpen(path string) error {
	return windows.ShellExecute(0, utf16Ptr("open"), utf16Ptr(path), nil, nil, windows.SW_SHOWNORMAL)
}

// ShellRun starts a program with arguments, e.g. ("control.exe", "mmsys.cpl,,1").
func ShellRun(file, args string) error {
	return windows.ShellExecute(0, utf16Ptr("open"), utf16Ptr(file), utf16Ptr(args), nil, windows.SW_SHOWNORMAL)
}

var (
	procSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree        = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemFree")
)

// browseInfo mirrors BROWSEINFOW (64 bytes on x64).
type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	LParam      uintptr
	Image       int32
}

const (
	bifReturnOnlyFSDirs = 0x0001
	bifNewDialogStyle   = 0x0040
)

// BrowseFolder shows the folder picker. ok is false when cancelled. Must run
// on the UI thread (which NewTray initialises for COM).
func BrowseFolder(owner uintptr, title string) (path string, ok bool) {
	display := make([]uint16, windows.MAX_PATH)
	bi := browseInfo{
		Owner:       owner,
		DisplayName: &display[0],
		Title:       utf16Ptr(title),
		Flags:       bifReturnOnlyFSDirs | bifNewDialogStyle,
	}
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", false
	}
	defer procCoTaskMemFree.Call(pidl)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	if r, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return "", false // not a file system folder
	}
	return windows.UTF16ToString(buf), true
}
