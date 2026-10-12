package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	sessionLifetime  = 180 * 24 * time.Hour // sliding: renewed while the phone is used
	pairCodeLifetime = 10 * time.Minute
	maxFailures      = 5           // wrong PINs before a lockout
	lockoutDuration  = time.Minute // per client address
	lastSeenPersist  = time.Hour   // how often LastSeen updates hit the disk
)

var (
	// ErrBadPIN means the PIN or pairing code was wrong.
	ErrBadPIN = errors.New("invalid PIN")
	// ErrLocked means too many wrong PINs came from this address.
	ErrLocked = errors.New("too many attempts")
)

type session struct {
	Hash     string    `json:"hash"` // SHA-256 of the token; the token itself is never stored
	Agent    string    `json:"agent,omitempty"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"lastSeen"`
}

type authState struct {
	PIN      string    `json:"pin"`
	Sessions []session `json:"sessions"`
}

type failure struct {
	count int
	until time.Time
}

// Auth manages the remote PIN, phone sessions and QR pairing codes. It
// persists to <dataDir>/remote.json and is safe for concurrent use.
type Auth struct {
	path string

	mu          sync.Mutex
	state       authState
	failures    map[string]*failure
	pairCode    string
	pairExpires time.Time
	now         func() time.Time

	// OnChange runs (on the calling goroutine) after a phone paired, logged
	// in or out, or the PIN was reset. Keep it quick.
	OnChange func()
}

// NewAuth loads or creates the remote auth state.
func NewAuth(dataDir string) (*Auth, error) {
	a := &Auth{path: filepath.Join(dataDir, "remote.json"), failures: map[string]*failure{}, now: time.Now}
	if data, err := os.ReadFile(a.path); err == nil {
		if err := json.Unmarshal(data, &a.state); err != nil {
			return nil, fmt.Errorf("reading %s: %w", a.path, err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropExpiredLocked()
	if len(a.state.PIN) != 6 {
		a.state.PIN = randomPIN()
		if err := a.saveLocked(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// PIN returns the current 6-digit PIN.
func (a *Auth) PIN() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state.PIN
}

// SessionCount returns the number of signed-in phones.
func (a *Auth) SessionCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropExpiredLocked()
	return len(a.state.Sessions)
}

// ResetAll creates a new PIN and signs out every phone.
func (a *Auth) ResetAll() error {
	a.mu.Lock()
	a.state.PIN = randomPIN()
	a.state.Sessions = nil
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

// Login checks a PIN from client address ip and returns a new session token.
func (a *Auth) Login(ip, pin, agent string) (string, error) {
	a.mu.Lock()
	f := a.failures[ip]
	if f != nil && a.now().Before(f.until) {
		a.mu.Unlock()
		return "", ErrLocked
	}
	if subtle.ConstantTimeCompare([]byte(pin), []byte(a.state.PIN)) != 1 {
		if f == nil {
			f = &failure{}
			a.failures[ip] = f
		}
		f.count++
		if f.count >= maxFailures {
			f.count = 0
			f.until = a.now().Add(lockoutDuration)
		}
		a.mu.Unlock()
		return "", ErrBadPIN
	}
	delete(a.failures, ip)
	token, err := a.newSessionLocked(agent)
	a.mu.Unlock()
	if err == nil {
		a.changed()
	}
	return token, err
}

// Pair redeems a one-time pairing code (from the QR code).
func (a *Auth) Pair(code, agent string) (string, error) {
	a.mu.Lock()
	if a.pairCode == "" || a.now().After(a.pairExpires) ||
		subtle.ConstantTimeCompare([]byte(code), []byte(a.pairCode)) != 1 {
		a.mu.Unlock()
		return "", ErrBadPIN
	}
	a.pairCode = "" // single use; the panel shows a fresh one
	token, err := a.newSessionLocked(agent)
	a.mu.Unlock()
	if err == nil {
		a.changed()
	}
	return token, err
}

// Valid reports whether token belongs to a live session and renews it.
func (a *Auth) Valid(token string) bool {
	if token == "" {
		return false
	}
	h := hashToken(token)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for i := range a.state.Sessions {
		s := &a.state.Sessions[i]
		if subtle.ConstantTimeCompare([]byte(s.Hash), []byte(h)) != 1 {
			continue
		}
		if now.Sub(s.LastSeen) > sessionLifetime {
			return false
		}
		if now.Sub(s.LastSeen) > lastSeenPersist {
			s.LastSeen = now
			_ = a.saveLocked()
		}
		return true
	}
	return false
}

// Logout ends the session of token.
func (a *Auth) Logout(token string) {
	h := hashToken(token)
	a.mu.Lock()
	kept := a.state.Sessions[:0]
	for _, s := range a.state.Sessions {
		if s.Hash != h {
			kept = append(kept, s)
		}
	}
	removed := len(kept) != len(a.state.Sessions)
	a.state.Sessions = kept
	if removed {
		_ = a.saveLocked()
	}
	a.mu.Unlock()
	if removed {
		a.changed()
	}
}

func (a *Auth) newSessionLocked(agent string) (string, error) {
	token := randomToken(32)
	now := a.now()
	if len(agent) > 200 {
		agent = agent[:200]
	}
	a.state.Sessions = append(a.state.Sessions, session{Hash: hashToken(token), Agent: agent, Created: now, LastSeen: now})
	return token, a.saveLocked()
}

func (a *Auth) dropExpiredLocked() {
	kept := a.state.Sessions[:0]
	for _, s := range a.state.Sessions {
		if a.now().Sub(s.LastSeen) <= sessionLifetime {
			kept = append(kept, s)
		}
	}
	a.state.Sessions = kept
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

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomPIN() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}
