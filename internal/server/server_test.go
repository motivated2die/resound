package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/motivated2die/resound/internal/audio"
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
	if vol["localVolume"] != float64(59) || vol["remoteVolume"] != float64(150) || vol["sliderPosition"] != float64(25) {
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

	// Sorting: kept beside the custom order, which it never changes; an app
	// from before sorting (no "sort") leaves it as it is.
	rec = do(t, h, "POST", "/api/layouts/favorites", `{"mode":"list","sort":"name-desc","orders":{"list":[`+jsonID(soundID)+`]}}`, c)
	if lay := s.lib.Layout("favorites"); rec.Code != http.StatusOK || lay.Sort != library.SortNameDesc || len(lay.Orders["list"]) != 1 ||
		!strings.Contains(rec.Body.String(), `"sort":"name-desc"`) {
		t.Fatalf("sort: %d %s", rec.Code, rec.Body)
	}
	do(t, h, "POST", "/api/layouts/favorites", `{"mode":"grid-2","orders":{"list":[`+jsonID(soundID)+`]}}`, c)
	if lay := s.lib.Layout("favorites"); lay.Sort != library.SortNameDesc || lay.Mode != "grid-2" {
		t.Fatalf("a layout without sort changed it: %+v", lay)
	}
	if rec := do(t, h, "POST", "/api/layouts/favorites", `{"mode":"list","sort":"a;b"}`, c); rec.Code != http.StatusBadRequest {
		t.Fatalf("accepted a bad sort mode: %d", rec.Code)
	}
	do(t, h, "POST", "/api/layouts/favorites", `{"mode":"list","sort":"","orders":{"list":[`+jsonID(soundID)+`]}}`, c)
	if lay := s.lib.Layout("favorites"); lay.Sort != "" || len(lay.Orders["list"]) != 1 {
		t.Fatalf("back to the custom order: %+v", lay)
	}
	rec = do(t, h, "GET", "/api/library", "", c)
	if !strings.Contains(rec.Body.String(), `"sort":""`) {
		t.Fatalf("the library doesn't send the sorting: %s", rec.Body)
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
	// +25 of 50 makes both defaults (50%, 100%) 1.5 times as loud: 59%, 150%.
	if len(player.volume) != 2 || player.volume[0] == nil || *player.volume[0] != 59 || *player.volume[1] != 150 {
		t.Fatalf("applied %v", player.volume)
	}
	do(t, h, "POST", path+"/reset", "", c)
	if player.volume[0] != nil || player.volume[1] != nil {
		t.Fatal("reset should return to the defaults")
	}
}

// The cards and the sheet get back the slider position a custom volume was
// set with, whatever the defaults. Below 10% the volumes are whole
// percentages of a linear fade (6 dB from 1% to 2%), and above it one percent
// can span two positions, so several positions give the same volume; the
// slider then shows one of them.
func TestSliderPosition(t *testing.T) {
	vol := func(v int) *int { return &v }
	if got := sliderPosition(library.Sound{}, 50, 100); got != 0 {
		t.Errorf("no custom volume: slider %d, want 0", got)
	}
	for _, defs := range [][2]int{{50, 100}, {100, 50}, {100, 100}, {30, 80}, {0, 100}, {100, 0}, {10, 10}} {
		louder := max(defs[0], defs[1])
		for pos := -50; pos <= 50; pos++ {
			local, remote := scaleVolume(defs[0], pos), scaleVolume(defs[1], pos)
			snd := library.Sound{SoundSettings: library.SoundSettings{LocalVolume: vol(local), RemoteVolume: vol(remote)}}
			got := sliderPosition(snd, defs[0], defs[1])
			custom := max(local, remote)
			switch d := got - pos; {
			case (pos < 0) != (got < 0) || scaleVolume(louder, got) != custom:
				t.Errorf("defaults %v, slider %d → %d%%, %d%% → slider %d, which gives %d%%",
					defs, pos, local, remote, got, scaleVolume(louder, got))
			case custom >= 10 && (d < -2 || d > 2):
				t.Errorf("defaults %v, slider %d → %d%%, %d%% → slider %d", defs, pos, local, remote, got)
			}
		}
	}
}

// All the way left silences the sound on both outputs; anywhere else, both
// outputs get louder or quieter by the same amount, as the sheet says ("for
// you and for others alike"), whatever the two default volumes; within
// 0.5 dB while both volumes are 10% or more, and within 3 dB in the fade
// below (whole percentages, more than 36 dB below full scale) down to 3%.
func TestSliderMovesBothOutputsAlike(t *testing.T) {
	for _, defs := range [][2]int{{50, 100}, {100, 50}, {20, 100}, {100, 100}, {75, 35}} {
		if l, r := scaleVolume(defs[0], -50), scaleVolume(defs[1], -50); l != 0 || r != 0 {
			t.Errorf("defaults %v: -50 gives %d%%, %d%%, want silence on both", defs, l, r)
		}
		for pos := -49; pos <= 50; pos++ {
			l, r := scaleVolume(defs[0], pos), scaleVolume(defs[1], pos)
			if min(l, r) < 3 {
				continue // 1% and 2% are 6 dB apart: below −46 dB, inaudible in voice chat
			}
			change := func(def, custom int) float64 {
				return 20 * math.Log10(float64(audio.Gain(custom)/audio.Gain(def)))
			}
			limit := 0.5
			if min(l, r) < 10 {
				limit = 3
			}
			if cl, cr := change(defs[0], l), change(defs[1], r); math.Abs(cl-cr) > limit {
				t.Errorf("defaults %v, slider %d: %.1f dB for you, %.1f dB for others", defs, pos, cl, cr)
			}
		}
	}
	// The old model multiplied the percentages: with these defaults, −40
	// was 12 dB quieter for the user but 32 dB for others.
	for _, def := range []int{75, 100} {
		if got := 20 * math.Log10(float64(audio.Gain(scaleVolume(def, -40))/audio.Gain(def))); math.Abs(got+32) > 1 {
			t.Errorf("default %d%%, slider -40: %.1f dB, want -32 dB", def, got)
		}
	}
}

// The phone's tab order is stored, applied to /api/tabs and /api/library,
// and validated.
func TestTabOrder(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, d, d+".mp3"), []byte(d), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lib := library.Open(t.TempDir(), []string{".mp3"})
	t.Cleanup(func() { _ = lib.Close() })
	lib.Scan([]string{root})
	auth, err := NewAuth(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(lib, &fakePlayer{lib: lib}, auth, t.Logf)
	h, c := s.Handler(), login(t, s)
	ids := map[string]uint64{}
	for _, tab := range lib.Tabs() {
		ids[tab.Name] = tab.ID
	}
	names := func(path string) string {
		t.Helper()
		var tabs []struct {
			Name string `json:"name"`
		}
		body := do(t, h, "GET", path, "", c).Body.Bytes()
		if path == "/api/library" {
			var lib struct {
				Tabs json.RawMessage `json:"tabs"`
			}
			_ = json.Unmarshal(body, &lib)
			body = lib.Tabs
		}
		if err := json.Unmarshal(body, &tabs); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var out []string
		for _, tab := range tabs {
			out = append(out, tab.Name)
		}
		return strings.Join(out, ",")
	}

	body := fmt.Sprintf(`{"order":[%d,%d,%d,99]}`, ids["c"], ids["a"], ids["b"])
	rec := do(t, h, "POST", "/api/library/tab-order", body, c)
	var resp struct {
		Order []uint64 `json:"order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK ||
		len(resp.Order) != 3 || resp.Order[0] != ids["c"] {
		t.Fatalf("tab-order: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/api/tabs", "/api/library"} {
		if got := names(path); got != "c,a,b" {
			t.Errorf("%s: tabs %s, want c,a,b", path, got)
		}
	}
	for _, bad := range []string{`{"order":"x"}`, `{"order":[-1]}`, `nonsense`} {
		if rec := do(t, h, "POST", "/api/library/tab-order", bad, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	if rec := do(t, h, "POST", "/api/library/tab-order", `{"order":[]}`, ""); rec.Code == http.StatusOK {
		t.Error("tab order accepted without a paired device")
	}
	do(t, h, "POST", "/api/library/reset-appearance", "", c)
	if got := names("/api/tabs"); got != "a,b,c" {
		t.Errorf("after reset: %s", got)
	}
}
