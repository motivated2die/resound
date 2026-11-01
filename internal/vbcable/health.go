package vbcable

import (
	"regexp"
	"strconv"
)

// Health is what Windows says about VB-Cable right now.
type Health int

const (
	// HealthMissing: no driver and no devices. Installing is safe.
	HealthMissing Health = iota
	// HealthReady: the driver runs without a problem and its devices are up.
	HealthReady
	// HealthPending: the driver is there, but Windows hasn't finished with
	// it: it reports a problem (usually "restart needed") or its devices
	// haven't appeared. Never install over this: VB-Audio asks for a restart
	// between. Restarting the device may be enough (Wake).
	HealthPending
	// HealthRemoved: the driver is gone but its devices linger, as right
	// after an uninstall. Only a Windows restart clears them.
	HealthRemoved
)

// Driver is VB-Cable's device as Windows' device manager sees it.
type Driver struct {
	Present  bool
	Instance string // device instance ID, e.g. ROOT\MEDIA\0003
	Problem  uint32 // Windows' problem code, 0 = none (14 = restart needed)
}

// Classify judges the driver together with whether the cable's two ends
// (CABLE Input and CABLE Output) are available.
func Classify(d Driver, endpoints bool) Health {
	switch {
	case !d.Present && !endpoints:
		return HealthMissing
	case !d.Present:
		return HealthRemoved
	case d.Problem != 0 || !endpoints:
		return HealthPending
	}
	return HealthReady
}

// knownPack is the number of the driver pack in PackURL.
const knownPack = 45

var packLink = regexp.MustCompile(`https://download\.vb-audio\.com/Download_CABLE/VBCABLE_Driver_Pack(\d{1,4})\.zip`)

// newestPack picks the highest driver pack linked on VB-Audio's page, if it
// is newer than the known one. "" means the known one (PackURL) is current.
func newestPack(page string) string {
	best, url := knownPack, ""
	for _, m := range packLink.FindAllStringSubmatch(page, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > best {
			best, url = n, m[0]
		}
	}
	return url
}
