package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	deviceLifetime   = 180 * 24 * time.Hour // sliding: renewed while the phone is used
	pairCodeLifetime = 10 * time.Minute
	lastSeenPersist  = time.Hour // how often LastSeen updates hit the disk
)

// ErrBadCode means the pairing code was wrong, used or expired.
var ErrBadCode = errors.New("invalid pairing code")

// Device is a phone (or browser) paired with the app. Its key is derived from
// DeviceSecret, never stored.
type Device struct {
	ID       string    `json:"id"`
	Agent    string    `json:"agent,omitempty"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"lastSeen"`
}

// authState is remote.json. Files of versions with the PIN-based browser
// remote also have "pin" and "sessions"; they are dropped on the next save.
type authState struct {
	// DeviceSecret derives the devices' keys; replacing it signs them all out.
	DeviceSecret string   `json:"deviceSecret,omitempty"`
	Devices      []Device `json:"devices,omitempty"`
	// RTCCert is the app connection's certificate and key (PEM).
	// Its fingerprint is in the pairing QR code, so it is kept across starts.
	RTCCert string `json:"rtcCert,omitempty"`
}

// Auth manages paired devices and the one-time pairing codes. It persists
// to <dataDir>/remote.json and is safe for concurrent use.
type Auth struct {
	path string

	mu          sync.Mutex
	state       authState
	pairCode    string
	pairExpires time.Time
	now         func() time.Time

	// OnChange runs (on the calling goroutine) after a device paired or was
	// signed out. Keep it quick.
	OnChange func()
}

// NewAuth loads or creates the pairing state.
func NewAuth(dataDir string) (*Auth, error) {
	a := &Auth{path: filepath.Join(dataDir, "remote.json"), now: time.Now}
	if data, err := os.ReadFile(a.path); err == nil {
		if err := json.Unmarshal(data, &a.state); err != nil {
			return nil, fmt.Errorf("reading %s: %w", a.path, err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropExpiredLocked()
	if a.state.DeviceSecret == "" {
		a.state.DeviceSecret = randomHex(32)
		if err := a.saveLocked(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// DeviceCount returns the number of paired devices.
func (a *Auth) DeviceCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropExpiredLocked()
	return len(a.state.Devices)
}

// Devices returns the paired devices, the most recently used first.
// LastSeen is renewed at most hourly while a device connects.
func (a *Auth) Devices() []Device {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropExpiredLocked()
	out := slices.Clone(a.state.Devices)
	slices.SortStableFunc(out, func(x, y Device) int { return y.LastSeen.Compare(x.LastSeen) })
	return out
}

// ResetAll signs out every device: their keys no longer work.
func (a *Auth) ResetAll() error {
	a.mu.Lock()
	a.state.DeviceSecret = randomHex(32)
	a.state.Devices = nil
	a.pairCode = ""
	err := a.saveLocked()
	a.mu.Unlock()
	a.changed()
	return err
}

// PairCode returns the current one-time pairing code, creating a new one
// when none exists or the old one expired.
func (a *Auth) PairCode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pairCode == "" || a.now().After(a.pairExpires) {
		a.pairCode = randomToken(18)
		a.pairExpires = a.now().Add(pairCodeLifetime)
	}
	return a.pairCode
}

func (a *Auth) dropExpiredLocked() {
	devices := a.state.Devices[:0]
	for _, d := range a.state.Devices {
		if a.now().Sub(d.LastSeen) <= deviceLifetime {
			devices = append(devices, d)
		}
	}
	a.state.Devices = devices
}

// PairPassword returns the ICE password of the current pairing code, which
// the app proves it knows (from the QR code) before a connection is set up.
// ok is false when no code is active.
func (a *Auth) PairPassword() (pwd string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pairCode == "" || a.now().After(a.pairExpires) {
		return "", false
	}
	return pairPassword(a.pairCode), true
}

// pairPassword turns a pairing code into an ICE password; the app does the
// same (rtc.js). ICE passwords allow only letters, digits, + and /.
func pairPassword(code string) string {
	sum := sha256.Sum256([]byte("resound-pair:" + code))
	return hex.EncodeToString(sum[:16])
}

// DeviceKey returns the key of a paired app device, which is also its ICE
// password, and renews the device.
func (a *Auth) DeviceKey(id string) (key string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for i := range a.state.Devices {
		d := &a.state.Devices[i]
		if d.ID != id {
			continue
		}
		if now.Sub(d.LastSeen) > deviceLifetime {
			return "", false
		}
		if now.Sub(d.LastSeen) > lastSeenPersist {
			d.LastSeen = now
			_ = a.saveLocked()
		}
		return a.deviceKeyLocked(id), true
	}
	return "", false
}

func (a *Auth) deviceKeyLocked(id string) string {
	mac := hmac.New(sha256.New, []byte(a.state.DeviceSecret))
	mac.Write([]byte("device:" + id))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// PairDevice redeems a one-time pairing code and returns the new
// device's ID and key.
func (a *Auth) PairDevice(code, agent string) (id, key string, err error) {
	a.mu.Lock()
	if a.pairCode == "" || a.now().After(a.pairExpires) ||
		subtle.ConstantTimeCompare([]byte(code), []byte(a.pairCode)) != 1 {
		a.mu.Unlock()
		return "", "", ErrBadCode
	}
	a.pairCode = ""
	if len(agent) > 200 {
		agent = agent[:200]
	}
	id = randomHex(6)
	now := a.now()
	a.state.Devices = append(a.state.Devices, Device{ID: id, Agent: agent, Created: now, LastSeen: now})
	key = a.deviceKeyLocked(id)
	err = a.saveLocked()
	a.mu.Unlock()
	if err == nil {
		a.changed()
	}
	return id, key, err
}

// ForgetDevice signs out a device (it asked to, or it was removed).
func (a *Auth) ForgetDevice(id string) {
	a.mu.Lock()
	kept := a.state.Devices[:0]
	for _, d := range a.state.Devices {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	removed := len(kept) != len(a.state.Devices)
	a.state.Devices = kept
	if removed {
		_ = a.saveLocked()
	}
	a.mu.Unlock()
	if removed {
		a.changed()
	}
}

// rtcCertPEM returns the stored certificate, creating one with create
// when there is none yet.
func (a *Auth) rtcCertPEM(create func() (string, error)) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.RTCCert != "" {
		return a.state.RTCCert, nil
	}
	key, err := create()
	if err != nil {
		return "", err
	}
	a.state.RTCCert = key
	return key, a.saveLocked()
}

func (a *Auth) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

func (a *Auth) changed() {
	if a.OnChange != nil {
		a.OnChange()
	}
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
