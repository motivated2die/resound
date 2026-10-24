package input

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Apps: which program a window belongs to, for bindings of its own
// (Keymap), and which programs run, for choosing them in the panel. Programs
// are known by their file name in lower case ("valorant.exe"): it stays the
// same across updates and installs, unlike the path.

// App is a program the user can give keybinds of its own.
type App struct {
	Exe  string // file name in lower case, e.g. "valorant.exe"
	Name string // its description from the file, else the file name without ".exe"
}

var (
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procGetWindow             = user32.NewProc("GetWindow")
	procGetWindowLongPtrW     = user32.NewProc("GetWindowLongPtrW")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procDwmGetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
)

const (
	gwOwner         = 4
	gwlExStyle      = -20
	wsExToolWindow  = 0x00000080
	dwmwaCloaked    = 14
	processQueryLtd = windows.PROCESS_QUERY_LIMITED_INFORMATION
)

// shellApps are parts of Windows with windows of their own that are no
// app to bind keys for.
var shellApps = map[string]bool{
	"explorer.exe": true, "applicationframehost.exe": true, "textinputhost.exe": true,
	"shellexperiencehost.exe": true, "startmenuexperiencehost.exe": true, "searchhost.exe": true,
	"lockapp.exe": true, "systemsettings.exe": true,
}

// appOfWindow returns the file name, in lower case, of the program that
// owns window hwnd ("" when it can't be told); own reports one of
// Resound's own windows.
func appOfWindow(hwnd uintptr) (app string, own bool) {
	var pid uint32
	windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	switch {
	case pid == 0:
		return "", false
	case pid == uint32(os.Getpid()):
		return "", true
	}
	return AppName(processPath(pid)), false
}

// AppName is the name a program is known by: its file name in lower case.
func AppName(path string) string {
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.Base(path))
}

// processPath returns the full path of process pid's program, "" when it
// can't be read (a protected process).
func processPath(pid uint32) string {
	h, err := windows.OpenProcess(processQueryLtd, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH * 2]uint16
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// RunningApps returns the file names, in lower case, of all running
// programs.
func RunningApps() map[string]bool {
	running := map[string]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return running
	}
	defer windows.CloseHandle(snap)
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		running[strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))] = true
	}
	return running
}

// windowApps collects the programs of the windows EnumWindows visits.
var (
	windowAppsMu  sync.Mutex
	windowAppPIDs map[uint32]bool
)

var enumWindowsCallback = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
	if appWindow(hwnd) {
		var pid uint32
		windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
		windowAppPIDs[pid] = true
	}
	return 1 // go on
})

// appWindow reports whether hwnd is a window as the taskbar shows it: a
// visible, titled, unowned top-level window that isn't a tool window and
// isn't cloaked (the hidden windows of suspended Store apps).
func appWindow(hwnd uintptr) bool {
	if !windows.IsWindowVisible(windows.HWND(hwnd)) {
		return false
	}
	if owner, _, _ := procGetWindow.Call(hwnd, gwOwner); owner != 0 {
		return false
	}
	exStyle := int32(gwlExStyle)
	ex, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(exStyle))
	if ex&wsExToolWindow != 0 {
		return false
	}
	if n, _, _ := procGetWindowTextLengthW.Call(hwnd); n == 0 {
		return false
	}
	var cloaked uint32
	if r, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked)); r == 0 && cloaked != 0 {
		return false
	}
	return true
}

// WindowApps returns the programs that show a window now, sorted by name,
// without Resound and the parts of Windows' shell.
func WindowApps() []App {
	windowAppsMu.Lock()
	windowAppPIDs = map[uint32]bool{}
	windows.EnumWindows(enumWindowsCallback, nil)
	pids := windowAppPIDs
	windowAppsMu.Unlock()

	seen := map[string]bool{}
	var apps []App
	for pid := range pids {
		if pid == uint32(os.Getpid()) {
			continue
		}
		path := processPath(pid)
		exe := AppName(path)
		if exe == "" || seen[exe] || shellApps[exe] {
			continue
		}
		seen[exe] = true
		apps = append(apps, App{Exe: exe, Name: fileDescription(path, exe)})
	}
	slices.SortFunc(apps, func(a, b App) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return apps
}

// fileDescription is a program's name from its version resource: the file
// description ("Discord"), else the product name, else exe without ".exe".
func fileDescription(path, exe string) string {
	fallback := strings.TrimSuffix(exe, ".exe")
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return fallback
	}
	data := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])); err != nil {
		return fallback
	}
	langs := []string{"040904b0", "040904e4", "000004b0"}
	var tr *[2]uint16
	var n uint32
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&tr), &n) == nil && n >= 4 {
		langs = append([]string{strings.ToLower(hex4(tr[0]) + hex4(tr[1]))}, langs...)
	}
	for _, key := range []string{"FileDescription", "ProductName"} {
		for _, lang := range langs {
			var p *uint16
			if windows.VerQueryValue(unsafe.Pointer(&data[0]), `\StringFileInfo\`+lang+`\`+key, unsafe.Pointer(&p), &n) == nil && n > 1 {
				if s := strings.TrimSpace(windows.UTF16PtrToString(p)); s != "" {
					return s
				}
			}
		}
	}
	return fallback
}

func hex4(v uint16) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>12], digits[v>>8&0xF], digits[v>>4&0xF], digits[v&0xF]})
}
