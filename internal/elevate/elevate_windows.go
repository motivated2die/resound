// Package elevate runs this executable again with administrator rights
// (UAC prompt) for a single, short task, and reports its exit code. The tray
// app itself never runs elevated.
package elevate

import (
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrCancelled means the user declined the UAC prompt.
var ErrCancelled = errors.New("administrator approval was declined")

// ErrTimeout means the elevated process was still running at the timeout.
// It keeps running; only the wait ended.
var ErrTimeout = errors.New("the elevated process did not finish in time")

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo mirrors SHELLEXECUTEINFOW (112 bytes on x64).
type shellExecuteInfo struct {
	Size       uint32
	Mask       uint32
	Hwnd       uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	InstApp    uintptr
	IDList     uintptr
	Class      *uint16
	KeyClass   uintptr
	HotKey     uint32
	IconOrMon  uintptr
	Process    windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
)

// Run starts this executable elevated with the given (already quoted)
// parameters, waits up to timeout and returns its exit code. It blocks, so
// call it off the UI thread.
func Run(params string, timeout time.Duration) (uint32, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	return RunFile(exe, params, "", windows.SW_HIDE, timeout)
}

// RunFile starts another program elevated, in dir ("" = inherited) with the
// given window show state, waits up to timeout and returns its exit code.
// The UAC prompt names that program and its signer. It blocks, so call it
// off the UI thread.
func RunFile(file, params, dir string, show int32, timeout time.Duration) (uint32, error) {
	sei := shellExecuteInfo{
		Mask:       seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		Verb:       windows.StringToUTF16Ptr("runas"),
		File:       windows.StringToUTF16Ptr(file),
		Parameters: windows.StringToUTF16Ptr(params),
		Show:       show,
	}
	if dir != "" {
		sei.Directory = windows.StringToUTF16Ptr(dir)
	}
	sei.Size = uint32(unsafe.Sizeof(sei))
	if r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei))); r == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, ErrCancelled
		}
		return 0, fmt.Errorf("starting elevated helper: %w", callErr)
	}
	if sei.Process == 0 {
		return 0, errors.New("elevated helper started without a process handle")
	}
	defer windows.CloseHandle(sei.Process)

	ev, err := windows.WaitForSingleObject(sei.Process, uint32(timeout.Milliseconds()))
	if err != nil {
		return 0, err
	}
	if ev == uint32(windows.WAIT_TIMEOUT) {
		return 0, fmt.Errorf("%w (%v)", ErrTimeout, timeout)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(sei.Process, &code); err != nil {
		return 0, err
	}
	return code, nil
}
