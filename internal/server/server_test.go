package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/motivated2die/resound/internal/library"
)

type fakePlayer struct {
	played    []uint64
	lib       *library.Library
	err       error  // returned by Play when set
	volume    []*int // last SetSoundVolume call: local, remote
	panelOpen bool
}

func (f *fakePlayer) Play(id uint64, preview bool) (Started, error) {
	if _, _, ok := f.lib.Sound(id); !ok {
		return Started{}, ErrNotFound
	}
	if f.err != nil {
		return Started{}, f.err
	}
	f.played = append(f.played, id)
	return Started{PlayingID: 7, Length: 1500 * time.Millisecond}, nil
}
func (f *fakePlayer) StopAll()             {}
func (f *fakePlayer) Progress() []Progress { return nil }
func (f *fakePlayer) TogglePause() bool    { return true }
func (f *fakePlayer) SetTalkThrough(bool)  {}
func (f *fakePlayer) TogglePanel() bool {
	f.panelOpen = !f.panelOpen
	return f.panelOpen
}
func (f *fakePlayer) DefaultVolumes() (int, int) { return 50, 100 }
func (f *fakePlayer) SetSoundVolume(_ uint64, local, remote *int) {
	f.volume = []*int{local, remote}
}

func newTestServer(t *testing.T) (*Server, *fakePlayer, uint64) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "boom.mp3")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := library.Open(t.TempDir(), []string{".mp3"})
	t.Cleanup(func() { _ = lib.Close() })
	lib.Scan([]string{root})
	auth, err := NewAuth(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	player := &fakePlayer{lib: lib}
	return New(lib, player, auth, t.Logf), player, library.ID(path)
}

// withDevice marks a request as coming from a paired device's connection,
// as rtc.go does.
func withDevice(r *http.Request, device string) *http.Request {
	if device == "" {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), rtcDeviceKey{}, device))
}

func do(t *testing.T, h http.Handler, method, path, body, device string) *httptest.ResponseRecorder {
	t.Helper()
	req := withDevice(httptest.NewRequest(method, path, strings.NewReader(body)), device)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// login pairs a device and returns its ID.
func login(t *testing.T, s *Server) string {
	t.Helper()
	id, _, err := s.Auth().PairDevice(s.Auth().PairCode(), "test")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func jsonID(id uint64) string {
	b, _ := json.Marshal(id)
	return string(b)
}

// Only requests from a device's connection reach the API.
func TestUnauthenticatedAccess(t *testing.T) {
	s, player, soundID := newTestServer(t)
	h := s.Handler()
	if rec := do(t, h, "GET", "/api/tabs", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("api without device: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/sounds/"+jsonID(soundID)+"/play", "", ""); rec.Code != http.StatusUnauthorized || len(player.played) != 0 {
		t.Fatalf("play without device: %d", rec.Code)
	}
}

func TestAPI(t *testing.T) {
	s, player, soundID := newTestServer(t)
	h, c := s.Handler(), login(t, s)

	rec := do(t, h, "GET", "/api/tabs", "", c)
	var tabs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tabs); err != nil || len(tabs) != 1 {
		t.Fatalf("tabs: %d %s", rec.Code, rec.Body)
	}

	rec = do(t, h, "POST", "/api/sounds/"+jsonID(soundID)+"/play", "", c)
	var play map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &play)
	if rec.Code != http.StatusOK || play["playingId"] != float64(7) || len(player.played) != 1 {
		t.Fatalf("play: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "POST", "/api/sounds/123/play", "", c); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown sound: %d", rec.Code)
	}

	rec = do(t, h, "POST", "/api/sounds/"+jsonID(soundID)+"/volume", `{"sliderPosition":25}`, c)
	var vol map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &vol)
	if vol["localVolume"] != float64(75) || vol["remoteVolume"] != float64(150) || vol["sliderPosition"] != float64(25) {
		t.Fatalf("volume: %s", rec.Body)
	}

	rec = do(t, h, "POST", "/api/sounds/"+jsonID(soundID)+"/favorite", "", c)
	if !strings.Contains(rec.Body.String(), `"isFavorite":true`) {
		t.Fatalf("favorite: %s", rec.Body)
	}
	rec = do(t, h, "GET", "/api/favorites", "", c)
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("favorites: %s", rec.Body)
	}

	// The phone's long press on the logo opens the panel, the next closes it.
	for _, want := range []string{`"open":true`, `"open":false`} {
		if rec := do(t, h, "POST", "/api/panel/toggle", "", c); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("panel toggle: %d %s, want %s", rec.Code, rec.Body, want)
		}
	}

	do(t, h, "POST", "/api/auth/logout", "", c)
	if s.Auth().DeviceCount() != 0 {
		t.Fatal("device still paired after logout")
	}
}

func TestPairCodeIsSingleUse(t *testing.T) {
	s, _, _ := newTestServer(t)
	code := s.Auth().PairCode()
	if _, _, err := s.Auth().PairDevice(code, "a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Auth().PairDevice(code, "b"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("pairing code worked twice: %v", err)
	}
	if s.Auth().PairCode() == code {
		t.Fatal("pairing code was not rotated")
	}
	s.Auth().now = func() time.Time { return time.Now().Add(2 * pairCodeLifetime) }
	if _, _, err := s.Auth().PairDevice(s.Auth().pairCode, "c"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("expired pairing code worked: %v", err)
	}
}

func TestResetSignsOutEveryone(t *testing.T) {
	s, _, _ := newTestServer(t)
	id := login(t, s)
	if err := s.Auth().ResetAll(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Auth().DeviceKey(id); ok || s.Auth().DeviceCount() != 0 {
		t.Fatal("device survived reset")
	}
}

// remote.json of versions with the browser remote has a PIN and sessions:
// devices pair anew, nothing breaks.
func TestOldRemoteJSON(t *testing.T) {
	dir := t.TempDir()
	old := `{"pin":"123456","sessions":[{"hash":"ab","created":"2026-01-01T00:00:00Z","lastSeen":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(dir, "remote.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := NewAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.DeviceCount() != 0 || a.state.DeviceSecret == "" {
		t.Fatalf("state = %+v", a.state)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "remote.json"))
	if strings.Contains(string(data), "pin") {
		t.Fatalf("old fields kept: %s", data)
	}
}

func TestLibraryEndpointAndETag(t *testing.T) {
	s, _, soundID := newTestServer(t)
	h, c := s.Handler(), login(t, s)
	rec := do(t, h, "GET", "/api/library", "", c)
	var lib struct {
		Tabs []struct {
			ID     uint64   `json:"id"`
			Sounds []uint64 `json:"sounds"`
		} `json:"tabs"`
		Sounds []map[string]any `json:"sounds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &lib); err != nil || len(lib.Tabs) != 1 || len(lib.Sounds) != 1 || lib.Tabs[0].Sounds[0] != soundID {
		t.Fatalf("library: %d %s", rec.Code, rec.Body)
	}
	etag := rec.Header().Get("ETag")
	req := httptest.NewRequest("GET", "/api/library", nil)
	req = withDevice(req, c)
	req.Header.Set("If-None-Match", etag)
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, req)
	if r2.Code != http.StatusNotModified {
		t.Fatalf("unchanged library: %d", r2.Code)
	}
	do(t, h, "POST", "/api/sounds/"+jsonID(soundID)+"/appearance", `{"color":"red","emoji":"💥"}`, c)
	r3 := httptest.NewRecorder()
	h.ServeHTTP(r3, req)
	if r3.Code != http.StatusOK || !strings.Contains(r3.Body.String(), `"color":"red"`) {
		t.Fatalf("changed library not sent: %d %s", r3.Code, r3.Body)
	}
}

func TestAppearanceValidation(t *testing.T) {
	s, _, soundID := newTestServer(t)
	h, c := s.Handler(), login(t, s)
	for _, body := range []string{`{"color":"red;}"}`, `{"emoji":"\" onload"}`, `{"emoji":"` + strings.Repeat("x", 80) + `"}`} {
		if rec := do(t, h, "POST", "/api/sounds/"+jsonID(soundID)+"/appearance", body, c); rec.Code != http.StatusBadRequest {
			t.Fatalf("accepted %s: %d", body, rec.Code)
		}
	}
	rec := do(t, h, "POST", "/api/sounds/"+jsonID(soundID)+"/appearance", `{"color":"blue"}`, c)
	if !strings.Contains(rec.Body.String(), `"color":"blue"`) {
		t.Fatalf("color: %s", rec.Body)
	}
	rec = do(t, h, "POST", "/api/sounds/"+jsonID(soundID)+"/appearance", `{"color":"default"}`, c)
	if !strings.Contains(rec.Body.String(), `"color":""`) {
		t.Fatalf("default color should clear: %s", rec.Body)
	}
}

func TestLayoutAndLegacyImport(t *testing.T) {
	s, _, soundID := newTestServer(t)
	h, c := s.Handler(), login(t, s)
	snd, _, _ := s.lib.Sound(soundID)

	rec := do(t, h, "POST", "/api/layouts/favorites", `{"mode":"list","orders":{"list":[`+jsonID(soundID)+`]}}`, c)
	if rec.Code != http.StatusOK || s.lib.Layout("favorites").Mode != "list" {
		t.Fatalf("layout: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "POST", "/api/layouts/../etc", `{}`, c); rec.Code == http.StatusOK {
		t.Fatal("accepted a bad layout key")
	}

	// A phone from an earlier version sends its path-keyed settings.
	pathJSON, _ := json.Marshal(snd.Path)
	body := `{"sounds":[{"path":` + string(pathJSON) + `,"color":"green","emoji":"🎺"}],` +
		`"layouts":{"` + jsonID(snd.TabID) + `":{"mode":"grid-2","orders":{"grid-2":[` + string(pathJSON) + `]}}}}`
	rec = do(t, h, "POST", "/api/library/import", body, c)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"sounds":1`) {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	got, _, _ := s.lib.Sound(soundID)
	lay := s.lib.Layout(jsonID(snd.TabID))
	if got.Color != "green" || got.Emoji != "🎺" || lay.Mode != "grid-2" || len(lay.Orders["grid-2"]) != 1 || lay.Orders["grid-2"][0] != soundID {
		t.Fatalf("imported: %+v %+v", got.SoundSettings, lay)
	}
	// Without overwrite, a second phone does not replace existing choices.
	do(t, h, "POST", "/api/library/import", `{"sounds":[{"id":`+jsonID(soundID)+`,"color":"red"}]}`, c)
	if got, _, _ := s.lib.Sound(soundID); got.Color != "green" {
		t.Fatalf("import overwrote without overwrite: %s", got.Color)
	}
	do(t, h, "POST", "/api/library/reset-appearance", "", c)
	if got, _, _ := s.lib.Sound(soundID); got.Color != "" || got.Emoji != "" || s.lib.Layout("favorites").Mode != "" {
		t.Fatal("reset-appearance kept values")
	}
}

func TestPlayErrors(t *testing.T) {
	s, player, soundID := newTestServer(t)
	h, c := s.Handler(), login(t, s)
	path := "/api/sounds/" + strconv.FormatUint(soundID, 10) + "/play"

	// A damaged file is the sound's problem: 422 with a message for the phone.
	player.err = fmt.Errorf("decoding boom.mp3: mp3: EOF: %w", ErrUnplayable)
	rec := do(t, h, "POST", path, "", c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "missing or damaged") {
		t.Fatalf("damaged file: %d %s", rec.Code, rec.Body)
	}
	// Anything else stays a server error.
	player.err = errors.New("no output is running")
	if rec := do(t, h, "POST", path, "", c); rec.Code != http.StatusInternalServerError {
		t.Fatalf("other error: %d", rec.Code)
	}
}

func TestVolumeAppliesToPlayingSound(t *testing.T) {
	s, player, soundID := newTestServer(t)
	h, c := s.Handler(), login(t, s)
	path := "/api/sounds/" + strconv.FormatUint(soundID, 10) + "/volume"
	if rec := do(t, h, "POST", path, `{"sliderPosition":25}`, c); rec.Code != http.StatusOK {
		t.Fatalf("volume: %d %s", rec.Code, rec.Body)
	}
	// +25 of 50 scales both defaults (50, 100) by 1.5.
	if len(player.volume) != 2 || player.volume[0] == nil || *player.volume[0] != 75 || *player.volume[1] != 150 {
		t.Fatalf("applied %v", player.volume)
	}
	do(t, h, "POST", path+"/reset", "", c)
	if player.volume[0] != nil || player.volume[1] != nil {
		t.Fatal("reset should return to the defaults")
	}
}

// The cards show whether a custom volume is quieter or louder than the
// default ("You hear", here 50) by the slider position's sign.
func TestSliderPosition(t *testing.T) {
	vol := func(v int) *int { return &v }
	for _, c := range []struct {
		local *int
		want  int
	}{
		{nil, 0}, {vol(50), 0}, {vol(25), -25}, {vol(0), -50}, {vol(75), 25}, {vol(100), 50}, {vol(200), 50},
	} {
		snd := library.Sound{SoundSettings: library.SoundSettings{LocalVolume: c.local}}
		if got := sliderPosition(snd, 50); got != c.want {
			t.Errorf("local volume %v: slider %d, want %d", c.local, got, c.want)
		}
	}
}
