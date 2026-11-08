package vbcable

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/motivated2die/resound/internal/elevate"
	"github.com/motivated2die/resound/internal/logx"
)

// WakeCommand is the first argument that makes the executable run as the
// short-lived elevated helper of Wake instead of the tray app.
const WakeCommand = "--wake-cable"

// driverService is the service of VB-Cable's main device. VB-Cable's A/B
// variants and Voicemeeter's driver have services of their own, so they never
// count as this cable.
const driverService = "VBAudioVACMME"

// mediaClass is Windows' device class "Sound, video and game controllers".
var mediaClass = windows.GUID{Data1: 0x4d36e96c, Data2: 0xe325, Data3: 0x11ce,
	Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18}}

// ReadDriver finds VB-Cable's device and its state. No rights needed.
func ReadDriver() (Driver, error) {
	set, err := windows.SetupDiGetClassDevsEx(&mediaClass, "", 0, windows.DIGCF_PRESENT, 0, "")
	if err != nil {
		return Driver{}, fmt.Errorf("listing devices: %w", err)
	}
	defer set.Close()
	for i := 0; ; i++ {
		data, err := set.EnumDeviceInfo(i)
		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			return Driver{}, nil
		}
		if err != nil {
			return Driver{}, fmt.Errorf("reading device %d: %w", i, err)
		}
		service, err := set.DeviceRegistryProperty(data, windows.SPDRP_SERVICE)
		if name, ok := service.(string); err != nil || !ok || !strings.EqualFold(name, driverService) {
			continue
		}
		d := Driver{Present: true}
		if d.Instance, err = set.DeviceInstanceID(data); err != nil {
			return Driver{}, fmt.Errorf("reading the device's ID: %w", err)
		}
		var status, problem uint32
		if err := windows.CM_Get_DevNode_Status(&status, &problem, data.DevInst, 0); err != nil {
			return Driver{}, fmt.Errorf("reading the device's status: %w", err)
		}
		if status&windows.DN_HAS_PROBLEM != 0 {
			d.Problem = problem
		}
		return d, nil
	}
}

// CheckHealth judges VB-Cable, given whether its two ends are available. If
// Windows can't be asked about the driver, the ends decide alone.
func CheckHealth(endpoints bool) Health {
	d, err := ReadDriver()
	if err != nil {
		logx.Printf("vbcable: %v", err)
		if endpoints {
			return HealthReady
		}
		return HealthMissing
	}
	h := Classify(d, endpoints)
	if d.Problem != 0 {
		logx.Printf("vbcable: device %s reports problem %d", d.Instance, d.Problem)
	}
	return h
}

// wakeSteps are the pnputil runs of Wake: look for new devices, then restart
// only the cable's own device. Windows' audio services keep running, so
// nothing else on the PC is interrupted (restarting them drops other apps'
// sound and virtual devices such as SteelSeries Sonar's).
func wakeSteps(instance string) [][]string {
	return [][]string{{"/scan-devices"}, {"/restart-device", instance}}
}

// Wake asks Windows to bring up a cable that waits for a restart, through an
// elevated copy of this executable. It blocks until the helper exits. Success
// only means Windows took the request: judge the result with CheckHealth.
func Wake() error {
	code, err := elevate.Run(WakeCommand, 90*time.Second)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("the helper exited with code %d (see log)", code)
	}
	return nil
}

// RunWakeCommand is the entry point of the elevated helper. It returns the
// process exit code.
func RunWakeCommand() int {
	d, err := ReadDriver()
	if err != nil || !d.Present {
		logx.Printf("vbcable: nothing to wake: present=%v, %v", d.Present, err)
		return 1
	}
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		logx.Printf("vbcable: %v", err)
		return 1
	}
	pnputil := filepath.Join(dir, "pnputil.exe") // never one found elsewhere on the PATH
	code := 0
	for _, step := range wakeSteps(d.Instance) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, pnputil, step...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
		out, err := cmd.CombinedOutput()
		cancel()
		logx.Printf("vbcable: pnputil %q: %v: %s", step, err, strings.TrimSpace(string(out)))
		code = 0
		if err != nil {
			code = 1 // only the last step, the restart, decides
		}
	}
	return code
}
