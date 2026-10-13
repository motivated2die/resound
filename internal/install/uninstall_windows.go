package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/motivated2die/resound/internal/elevate"
	"github.com/motivated2die/resound/internal/logx"
	"github.com/motivated2die/resound/internal/ui"
)

const defaultStopTimeout = 5 * time.Second

// RunUninstall is the "--uninstall" entry point (Settings > Installed apps,
// or the panel). It asks first, stops the running copy and removes the app,
// its registrations and its data folder. closeLog is called before the data
// folder (which holds the log) is deleted. It returns the exit code.
func RunUninstall(dataDir string, closeLog func()) int {
	if !ui.Confirm(ui.DialogSpec{
		Title: "Uninstall Resound?",
		Message: "WARNING: This removes Resound, its Start Menu entry, autostart and its settings " +
			"(INCLUDING BACKUPS stored in the default folders, sound index and custom settings, devices, paired phones) Copy your backups if you want to restore them later. Your actual sound files are NOT deleted.\n\n" +
			"Windows sound settings Resound changed for you, like \u201cListen to this device\u201d " +
			"on your microphone, stay as they are.",
		Confirm: "Uninstall",
		Cancel:  "Keep Resound",
		Danger:  true,
	}) {
		logx.Printf("uninstall: cancelled")
		return 0
	}
	logx.Printf("uninstall: removing Resound")
	if !ui.SignalQuitRunningInstance(defaultStopTimeout) {
		logx.Printf("uninstall: the running copy did not exit in time")
	}

	var errs []error
	errs = append(errs, SetAutoStart(false), SetResumeOnce(false), deleteKey(uninstallKey))
	removeShortcut()

	closeLog()
	if err := os.RemoveAll(dataDir); err != nil {
		errs = append(errs, fmt.Errorf("removing %s: %w", dataDir, err))
	}
	if target, err := ExePath(); err == nil {
		deleteInstallFolder(target)
	}

	text, flags := "Resound has been uninstalled.", uint32(windows.MB_OK|windows.MB_ICONINFORMATION)
	if err := errors.Join(errs...); err != nil {
		text, flags = "Resound was uninstalled, but some parts could not be removed:\n\n"+err.Error(), windows.MB_OK|windows.MB_ICONWARNING
	}
	windows.MessageBox(0, windows.StringToUTF16Ptr(text), windows.StringToUTF16Ptr("Resound"), flags|windows.MB_SETFOREGROUND)
	return 0
}

// deleteInstallFolder removes the installed exe and its folder. A running
// image can't be deleted, so when this process is the installed copy a
// hidden cmd.exe finishes the job a moment after we exit.
func deleteInstallFolder(exePath string) {
	dir := filepath.Dir(exePath)
	if !strings.EqualFold(filepath.Base(dir), installDir) {
		return // never delete anything unexpected
	}
	if err := os.Remove(exePath); err == nil || errors.Is(err, os.ErrNotExist) {
		if os.RemoveAll(dir) == nil {
			return
		}
	}
	const delay = `ping -n 3 127.0.0.1 >nul`
	script := fmt.Sprintf(`%[1]s & del /f /q "%[2]s" & rmdir /s /q "%[3]s" & %[1]s & del /f /q "%[2]s" & rmdir /s /q "%[3]s"`,
		delay, exePath, dir)
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = `C:\Windows\System32\cmd.exe`
	}
	cmd := exec.Command(comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       fmt.Sprintf(`"%s" /d /s /c "%s"`, comspec, script),
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
}

// --- firewall ----------------------------------------------------------------

// FirewallCommand is the first argument of the elevated firewall helper.
const FirewallCommand = "--allow-firewall"

// AllowFirewall adds an inbound "allow" rule for this exe on private and
// domain networks through the elevated helper (UAC prompt). Call it off the
// UI thread.
func AllowFirewall() error {
	code, err := elevate.Run(FirewallCommand, 60*time.Second)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("firewall helper failed with code %d (see log)", code)
	}
	return nil
}

// RunFirewallCommand runs elevated: it removes every existing rule for this
// exe — including the "block" rules Windows creates when its own prompt is
// dismissed, which win over allow rules — and adds one allow rule.
func RunFirewallCommand() int {
	exe, err := currentExe()
	if err != nil {
		logx.Printf("firewall helper: %v", err)
		return 1
	}
	_ = netsh(fmt.Sprintf(`advfirewall firewall delete rule name=all program="%s"`, exe))
	if err := netsh(fmt.Sprintf(`advfirewall firewall add rule name="%s" dir=in action=allow program="%s" enable=yes profile=private,domain`, AppName, exe)); err != nil {
		logx.Printf("firewall helper: %v", err)
		return 1
	}
	logx.Printf("firewall helper: allowed %s on private and domain networks", exe)
	return 0
}

// netsh runs netsh with a raw command line (Go's argument quoting is not
// what netsh expects for program="...").
func netsh(args string) error {
	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	exe := filepath.Join(sys, "System32", "netsh.exe")
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       fmt.Sprintf(`"%s" %s`, exe, args),
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %s: %v: %s", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}
