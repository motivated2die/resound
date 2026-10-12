// Package vbcable installs VB-Audio's VB-CABLE for the user: it downloads
// the official driver pack from vb-audio.com, checks the setup's signature,
// runs it silently with administrator approval, and puts back the default
// audio devices if Windows switched them to the cable.
//
// VB-CABLE is donationware by VB-Audio Software (www.vb-cable.com). Its
// licensing page allows installing the unmodified package from another
// application, silently, as long as users can see that VB-CABLE is a
// VB-Audio product and can donate; the app shows both. The package is never
// bundled or modified: it comes straight from VB-Audio's server.
//
// A driver install needs a Windows restart to finish. Pending records an
// install across that restart, so the app can continue its setup.
package vbcable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const pendingFile = "vbcable-install.json"

// Return targets: the page the app reopens after the restart.
const (
	ReturnWizard = "wizard"
	ReturnVoice  = "voice"
)

// Defaults are the default audio endpoint IDs before the install, so a
// default that Windows moved to the cable can be put back.
type Defaults struct {
	Render       string `json:"render,omitempty"`
	RenderComms  string `json:"renderComms,omitempty"`
	Capture      string `json:"capture,omitempty"`
	CaptureComms string `json:"captureComms,omitempty"`
}

// Pending is an install that waits for a Windows restart.
type Pending struct {
	Started    time.Time `json:"started"`
	BootTime   time.Time `json:"bootTime"` // when Windows started, at install time
	ReturnTo   string    `json:"returnTo"`
	WizardStep int       `json:"wizardStep,omitempty"`
	Defaults   Defaults  `json:"defaults"`
}

// Rebooted reports whether Windows has restarted since the install, given
// the current boot time. Boot times are derived from the wall clock, so
// small differences (clock sync) are tolerated.
func (p Pending) Rebooted(bootTime time.Time) bool {
	return bootTime.Sub(p.BootTime) > 2*time.Minute
}

func pendingPath(dataDir string) string { return filepath.Join(dataDir, pendingFile) }

// SavePending writes the pending install atomically.
func SavePending(dataDir string, p Pending) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := pendingPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, pendingPath(dataDir))
}

// LoadPending reads the pending install; ok is false when there is none or
// the file is unreadable.
func LoadPending(dataDir string) (p Pending, ok bool) {
	data, err := os.ReadFile(pendingPath(dataDir))
	if err != nil {
		return Pending{}, false
	}
	if json.Unmarshal(data, &p) != nil || p.Started.IsZero() {
		return Pending{}, false
	}
	return p, true
}

// ClearPending removes the pending install.
func ClearPending(dataDir string) error {
	if err := os.Remove(pendingPath(dataDir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
