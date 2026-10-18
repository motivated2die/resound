// Package install makes Resound a "drop-in" app, like the Skira Seed
// Helper: running the downloaded exe copies it to
// %LOCALAPPDATA%\Programs\Resound, registers it under Settings > Installed
// apps, adds a Start Menu shortcut and autostart, then starts the installed
// copy with a welcome notice. No admin rights are needed for any of it.
//
// Development builds are "portable" and never install themselves (see
// Portable).
package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
	"github.com/motivated2die/resound/internal/update"
)

const (
	AppName     = "Resound"
	ExeName     = "Resound.exe"
	Publisher   = "Resound"
	installDir  = "Resound" // %LOCALAPPDATA%\Programs\Resound
	versionFile = "installed_version.txt"

	// WelcomeFlag makes the freshly installed copy show the install/update
	// notice: --welcome=install|update.
	WelcomeFlag    = "--welcome="
	WelcomeInstall = "install"
	WelcomeUpdate  = "update"

	// UninstallCommand is what Settings > Installed apps runs.
	UninstallCommand = "--uninstall"

	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue     = "Resound"
	runOnceKey   = `Software\Microsoft\Windows\CurrentVersion\RunOnce`
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Resound`
)

// ExePath returns %LOCALAPPDATA%\Programs\Resound\Resound.exe.
func ExePath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(base, "Programs", installDir, ExeName), nil
}

func currentExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// IsInstalledCopy reports whether this process runs from the install folder.
func IsInstalledCopy() bool {
	exe, err1 := currentExe()
	target, err2 := ExePath()
	return err1 == nil && err2 == nil && sameFile(exe, target)
}

// Portable reports whether this build must not install itself: when
// RESOUND_PORTABLE=1 is set, or a file named "portable" or a go.mod sits
// next to the exe (or one folder up) — i.e. a development build.
func Portable() bool {
	if os.Getenv("RESOUND_PORTABLE") == "1" {
		return true
	}
	exe, err := currentExe()
	if err != nil {
		return true
	}
	dir := filepath.Dir(exe)
	for _, p := range []string{
		filepath.Join(dir, "portable"),
		filepath.Join(dir, "go.mod"),
		filepath.Join(dir, "..", "go.mod"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func sameFile(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ai, bi)
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func versionPath(dataDir string) string { return filepath.Join(dataDir, versionFile) }

func installedVersion(dataDir string) string {
	data, err := os.ReadFile(versionPath(dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Ensure installs or updates when this exe runs from anywhere but the
// install folder. exit=true means this process was only the installer and
// should end now (the installed copy has been started).
func Ensure(version, dataDir string) (exit bool, err error) {
	exe, err := currentExe()
	if err != nil {
		return false, err
	}
	target, err := ExePath()
	if err != nil {
		return false, err
	}

	if sameFile(exe, target) {
		// The installed copy: refresh registrations after an in-place update.
		if installedVersion(dataDir) != version {
			_ = os.WriteFile(versionPath(dataDir), []byte(version), 0o644)
			logRegistration(target, version)
		}
		_ = os.Remove(target + ".old")
		return false, nil
	}

	previous := installedVersion(dataDir)
	if isDowngrade(previous, version, target) && !confirmDowngrade(previous, version) {
		logx.Printf("install: v%s is older than the installed v%s; starting that instead", version, previous)
		// A running instance opens its panel (single instance).
		if err := exec.Command(target).Start(); err != nil {
			return false, fmt.Errorf("starting the installed copy: %w", err)
		}
		return true, nil
	}
	logx.Printf("install: v%s to %s (installed: %q)", version, target, previous)
	if !ui.SignalQuitRunningInstance(defaultStopTimeout) {
		logx.Printf("install: the running copy did not exit in time; replacing it anyway")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	if err := copyExecutable(exe, target); err != nil {
		return false, fmt.Errorf("installing to %s: %w", target, err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err == nil {
		_ = os.WriteFile(versionPath(dataDir), []byte(version), 0o644)
	}
	logRegistration(target, version) // autostart stays off; the guided setup offers it

	var args []string
	switch previous {
	case "":
		args = []string{WelcomeFlag + WelcomeInstall}
	case version:
		// Same version again: just start it (no notice).
	default:
		args = []string{WelcomeFlag + WelcomeUpdate}
	}
	if err := exec.Command(target, args...).Start(); err != nil {
		return false, fmt.Errorf("starting the installed copy: %w", err)
	}
	return true, nil
}

// isDowngrade reports whether installing version would replace a newer
// installed copy (one that is still there).
func isDowngrade(installed, version, target string) bool {
	if !update.Newer(installed, version) {
		return false
	}
	_, err := os.Stat(target)
	return err == nil
}

// confirmDowngrade warns that an older version is about to replace the
// installed one. Starting the installed copy is the default, also when the
// dialog is closed; true means install the older version anyway.
func confirmDowngrade(installed, version string) bool {
	return ui.Confirm(ui.DialogSpec{
		Title: "Resound v" + installed + " is already installed",
		Message: "This is the older version v" + version + ". Installing it replaces the newer one, " +
			"and it may not work with the settings v" + installed + " saved.",
		Confirm: "Install v" + version + " anyway",
		Cancel:  "Start v" + installed,
		Danger:  true,
	})
}

func logRegistration(target, version string) {
	if err := registerInstalledApp(target, version); err != nil {
		logx.Printf("install: Installed apps entry: %v", err)
	}
	if err := createShortcut(target); err != nil {
		logx.Printf("install: Start Menu shortcut: %v", err)
	}
}

// copyExecutable copies src over dst. A running dst can't be overwritten
// but can be renamed, so that is the fallback.
func copyExecutable(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err == nil {
		return nil
	}
	old := dst + ".old"
	_ = os.Remove(old)
	if err := os.Rename(dst, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// --- registry ----------------------------------------------------------------

func registerInstalledApp(target, version string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return errors.Join(
		k.SetStringValue("DisplayName", AppName),
		k.SetStringValue("DisplayVersion", version),
		k.SetStringValue("DisplayIcon", target+",0"),
		k.SetStringValue("Publisher", Publisher),
		k.SetStringValue("InstallLocation", filepath.Dir(target)),
		k.SetStringValue("UninstallString", fmt.Sprintf(`"%s" %s`, target, UninstallCommand)),
		k.SetDWordValue("NoModify", 1),
		k.SetDWordValue("NoRepair", 1),
	)
}

// AutoStartEnabled reports whether Resound starts with Windows.
func AutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}

// SetAutoStart starts Resound (the installed copy, or this exe when
// portable) at sign-in, or stops doing so.
func SetAutoStart(on bool) error {
	if !on {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if err != nil {
			return nil
		}
		defer k.Close()
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	target, err := autoStartTarget()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, fmt.Sprintf(`"%s" --autostart`, target))
}

// ResumeFlag starts Resound once after a Windows restart (see
// SetResumeOnce) to continue the setup. The app reads the details from its
// own state, so the flag only marks the start in the log.
const ResumeFlag = "--resume-setup"

// SetResumeOnce starts Resound once at the next sign-in (HKCU RunOnce:
// Windows deletes the entry before running it), or removes that entry. It
// works alongside the normal autostart: the second start just opens the
// running instance's panel.
func SetResumeOnce(on bool) error {
	if !on {
		k, err := registry.OpenKey(registry.CURRENT_USER, runOnceKey, registry.SET_VALUE)
		if err != nil {
			return nil
		}
		defer k.Close()
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	target, err := autoStartTarget()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runOnceKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, fmt.Sprintf(`"%s" %s`, target, ResumeFlag))
}

var (
	procRegisterApplicationRestart   = windows.NewLazySystemDLL("kernel32.dll").NewProc("RegisterApplicationRestart")
	procUnregisterApplicationRestart = windows.NewLazySystemDLL("kernel32.dll").NewProc("UnregisterApplicationRestart")
)

// Restart flags: only restart after a Windows restart, never after a
// crash, a hang or an update of the app itself.
const restartOnlyAfterReboot = 1 | 2 | 4 // RESTART_NO_CRASH | RESTART_NO_HANG | RESTART_NO_PATCH

// RegisterRestart asks Windows to start this process again with ResumeFlag
// right after the sign-in that follows RestartWindows. That is faster than
// RunOnce, which Windows only runs after all other startup apps, but Windows
// skips it for processes that ran less than 60 seconds, so RunOnce stays as
// the fallback.
func RegisterRestart() error {
	if hr, _, _ := procRegisterApplicationRestart.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(ResumeFlag))), restartOnlyAfterReboot); hr != 0 {
		return fmt.Errorf("RegisterApplicationRestart: 0x%08x", uint32(hr))
	}
	return nil
}

// UnregisterRestart undoes RegisterRestart.
func UnregisterRestart() { procUnregisterApplicationRestart.Call() }

// RestartWindows asks Windows to restart, and to start the apps registered
// for it (RegisterRestart) again afterwards. Apps with unsaved work can
// still hold it up, as with a restart from the Start menu.
func RestartWindows() error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("opening the process token: %w", err)
	}
	defer token.Close()
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeShutdownPrivilege"), &luid); err != nil {
		return fmt.Errorf("looking up the shutdown privilege: %w", err)
	}
	privs := windows.Tokenprivileges{PrivilegeCount: 1}
	privs.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	if err := windows.AdjustTokenPrivileges(token, false, &privs, 0, nil, nil); err != nil {
		return fmt.Errorf("enabling the shutdown privilege: %w", err)
	}
	const reason = windows.SHTDN_REASON_MAJOR_APPLICATION | windows.SHTDN_REASON_MINOR_INSTALLATION | windows.SHTDN_REASON_FLAG_PLANNED
	return windows.ExitWindowsEx(windows.EWX_REBOOT|windows.EWX_RESTARTAPPS, reason)
}

// autoStartTarget is the exe that starts at sign-in: the installed copy, or
// this exe when portable.
func autoStartTarget() (string, error) {
	target, err := ExePath()
	if err != nil || !IsInstalledCopy() && Portable() {
		return currentExe()
	}
	return target, nil
}

func deleteKey(path string) error {
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("deleting HKCU\\%s: %w", path, err)
	}
	return nil
}

// --- Start Menu shortcut -----------------------------------------------------

func shortcutPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", errors.New("APPDATA is not set")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", AppName+".lnk"), nil
}

// createShortcut writes the Start Menu shortcut through WScript.Shell, on
// its own COM thread.
func createShortcut(target string) error {
	path, err := shortcutPath()
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
			var oleErr *ole.OleError
			if !errors.As(err, &oleErr) || oleErr.Code() != 1 { // S_FALSE = already initialised
				done <- fmt.Errorf("CoInitializeEx: %w", err)
				return
			}
		}
		defer ole.CoUninitialize()
		done <- writeShortcut(path, target)
	}()
	return <-done
}

func writeShortcut(path, target string) error {
	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return fmt.Errorf("WScript.Shell: %w", err)
	}
	defer unknown.Release()
	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer shell.Release()
	res, err := oleutil.CallMethod(shell, "CreateShortcut", path)
	if err != nil {
		return fmt.Errorf("CreateShortcut: %w", err)
	}
	sc := res.ToIDispatch()
	defer sc.Release()
	for _, p := range []struct{ name, value string }{
		{"TargetPath", target},
		{"WorkingDirectory", filepath.Dir(target)},
		{"IconLocation", target + ",0"},
		{"Description", "Soundboard you control from your phone"},
	} {
		if _, err := oleutil.PutProperty(sc, p.name, p.value); err != nil {
			return fmt.Errorf("shortcut %s: %w", p.name, err)
		}
	}
	_, err = oleutil.CallMethod(sc, "Save")
	return err
}

func removeShortcut() {
	if path, err := shortcutPath(); err == nil {
		_ = os.Remove(path)
	}
}
