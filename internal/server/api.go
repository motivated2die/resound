package server

import (
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/motivated2die/resound/internal/library"
)

// --- helpers ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"success": false, "error": msg})
}

func pathID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return 0, false
	}
	return id, true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(v)
}

// soundJSON is the list shape the front end reads: the Soundux Remote fields
// (id, name, path, isFavorite) plus the settings the phone shows on cards.
func soundJSON(s library.Sound) map[string]any {
	return map[string]any{
		"id": s.ID, "name": s.Name, "path": s.Path, "isFavorite": s.Favorite,
		"hasCustomVolume": s.LocalVolume != nil || s.RemoteVolume != nil,
		"color":           s.Color,
		"emoji":           s.Emoji,
		"durationMs":      s.Duration.Milliseconds(),
	}
}

// volumeInfo reproduces the Soundux Remote's per-sound volume model: one
// slider from -50 to +50 scales both defaults by 0x..2x.
func volumeInfo(s library.Sound, defLocal, defRemote int) map[string]any {
	ratio := 1.0
	if s.LocalVolume != nil {
		switch {
		case defLocal > 0:
			ratio = float64(*s.LocalVolume) / float64(defLocal)
		case *s.LocalVolume > 0:
			ratio = 2
		}
	}
	slider := 0
	switch {
	case ratio > 2:
		slider = 50
	case ratio < 0:
		slider = -50
	default:
		slider = int(math.Round((ratio - 1) * 50))
	}
	orNil := func(p *int) any {
		if p == nil {
			return nil
		}
		return *p
	}
	orDefault := func(p *int, d int) int {
		if p == nil {
			return d
		}
		return *p
	}
	return map[string]any{
		"hasCustomVolume":     s.LocalVolume != nil || s.RemoteVolume != nil,
		"localVolume":         orDefault(s.LocalVolume, defLocal),
		"remoteVolume":        orDefault(s.RemoteVolume, defRemote),
		"customLocalVolume":   orNil(s.LocalVolume),
		"customRemoteVolume":  orNil(s.RemoteVolume),
		"defaultLocalVolume":  defLocal,
		"defaultRemoteVolume": defRemote,
		"sliderPosition":      slider,
		"volumeRatio":         ratio,
	}
}

// --- auth ------------------------------------------------------------------

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PIN string `json:"pin"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	token, err := s.auth.Login(clientIP(r), body.PIN, r.UserAgent())
	switch {
	case errors.Is(err, ErrLocked):
		writeError(w, http.StatusTooManyRequests, "Too many attempts. Please wait a minute.")
		return
	case errors.Is(err, ErrBadPIN):
		s.logf("server: wrong PIN from %s", clientIP(r)) // the PIN itself is never logged
		writeError(w, http.StatusUnauthorized, "Invalid PIN. Please try again!")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "Could not create a session")
		return
	}
	s.dropOldSession(r)
	s.setSessionCookie(w, token)
	s.logf("server: phone signed in with PIN from %s", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// dropOldSession ends the session a re-pairing phone still carries, so one
// phone never counts as several.
func (s *Server) dropOldSession(r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		s.auth.Logout(c.Value)
	}
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if s.authenticated(r) {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
		return
	}
	writeJSON(w, http.StatusUnauthorized, map[string]any{"authenticated": false})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.auth.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	token, err := s.auth.Pair(r.URL.Query().Get("code"), r.UserAgent())
	if err != nil {
		s.logf("server: invalid or used pairing code from %s", clientIP(r))
		http.Redirect(w, r, "/login.html", http.StatusFound)
		return
	}
	s.dropOldSession(r)
	s.setSessionCookie(w, token)
	s.logf("server: phone paired by QR code from %s", clientIP(r))
	http.Redirect(w, r, "/", http.StatusFound)
}

// --- library ---------------------------------------------------------------

func (s *Server) handleTabs(w http.ResponseWriter, r *http.Request) {
	s.lib.Rescan(5 * time.Second) // a reload on the phone picks up new files
	tabs := []map[string]any{}
	for _, t := range s.lib.Tabs() {
		tabs = append(tabs, map[string]any{"id": t.ID, "name": t.Name, "path": t.Path, "sortMode": 0})
	}
	writeJSON(w, http.StatusOK, tabs)
}

func (s *Server) handleTabSounds(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	tab, found := s.lib.Tab(id)
	if !found {
		writeError(w, http.StatusNotFound, "Tab not found")
		return
	}
	sounds := []map[string]any{}
	for _, snd := range tab.Sounds {
		sounds = append(sounds, soundJSON(snd))
	}
	writeJSON(w, http.StatusOK, sounds)
}

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	sounds := []map[string]any{}
	for _, snd := range s.lib.Favorites() {
		sounds = append(sounds, soundJSON(snd))
	}
	writeJSON(w, http.StatusOK, sounds)
}

func (s *Server) handleSound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	snd, tabName, found := s.lib.Sound(id)
	if !found {
		writeError(w, http.StatusNotFound, "Sound not found")
		return
	}
	defLocal, defRemote := s.player.DefaultVolumes()
	resp := volumeInfo(snd, defLocal, defRemote)
	for k, v := range soundJSON(snd) {
		resp[k] = v
	}
	resp["tabName"] = tabName
	resp["tabId"] = snd.TabID
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSoundSettings(w http.ResponseWriter, r *http.Request) {
	defLocal, defRemote := s.player.DefaultVolumes()
	favorites := []uint64{}
	custom := map[string]any{}
	for _, t := range s.lib.Tabs() {
		for _, snd := range t.Sounds {
			if snd.Favorite {
				favorites = append(favorites, snd.ID)
			}
			if snd.LocalVolume != nil || snd.RemoteVolume != nil {
				custom[strconv.FormatUint(snd.ID, 10)] = volumeInfo(snd, defLocal, defRemote)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":             true,
		"favorites":           favorites,
		"customVolumes":       custom,
		"defaultLocalVolume":  defLocal,
		"defaultRemoteVolume": defRemote,
		"syncVolumes":         false,
	})
}

// --- playback --------------------------------------------------------------

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request, preview bool) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	started, err := s.player.Play(id, preview)
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "Sound not found")
		return
	case errors.Is(err, ErrUnplayable):
		s.logf("server: play %d failed: %v", id, err)
		writeError(w, http.StatusUnprocessableEntity, "The file is missing or damaged.")
		return
	case err != nil:
		s.logf("server: play %d failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Failed to play sound")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"id":         id,
		"playingId":  started.PlayingID,
		"lengthInMs": started.Length.Milliseconds(),
	})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.player.StopAll()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, p := range s.player.Progress() {
		out = append(out, map[string]any{
			"id":         p.PlayingID,
			"soundId":    p.SoundID,
			"name":       p.Name,
			"lengthInMs": p.Length.Milliseconds(),
			"readInMs":   p.Position.Milliseconds(),
			"paused":     p.Paused,
			"repeat":     false,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	state := "playing"
	if s.player.TogglePause() {
		state = "paused"
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "newState": state})
}

func (s *Server) handleTalk(w http.ResponseWriter, on bool) {
	s.player.SetTalkThrough(on)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// --- per-sound settings ----------------------------------------------------

func (s *Server) handleFavorite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	snd, found, err := s.lib.Update(id, func(st *library.SoundSettings) { st.Favorite = !st.Favorite })
	if !found {
		writeError(w, http.StatusNotFound, "Sound not found")
		return
	}
	if err != nil {
		s.logf("server: saving favorite: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "isFavorite": snd.Favorite})
}

func (s *Server) handleVolume(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		SliderPosition int `json:"sliderPosition"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	pos := max(-50, min(50, body.SliderPosition))
	defLocal, defRemote := s.player.DefaultVolumes()
	factor := 1 + float64(pos)/50
	scale := func(d int) *int {
		v := max(0, min(200, int(math.Round(float64(d)*factor))))
		return &v
	}
	snd, found, err := s.lib.Update(id, func(st *library.SoundSettings) {
		if pos == 0 {
			st.LocalVolume, st.RemoteVolume = nil, nil
		} else {
			st.LocalVolume, st.RemoteVolume = scale(defLocal), scale(defRemote)
		}
	})
	s.respondVolume(w, snd, found, err, defLocal, defRemote)
}

func (s *Server) handleVolumeReset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	defLocal, defRemote := s.player.DefaultVolumes()
	snd, found, err := s.lib.Update(id, func(st *library.SoundSettings) { st.LocalVolume, st.RemoteVolume = nil, nil })
	s.respondVolume(w, snd, found, err, defLocal, defRemote)
}

func (s *Server) respondVolume(w http.ResponseWriter, snd library.Sound, found bool, err error, defLocal, defRemote int) {
	if !found {
		writeError(w, http.StatusNotFound, "Sound not found")
		return
	}
	s.player.SetSoundVolume(snd.ID, snd.LocalVolume, snd.RemoteVolume) // heard at once if it plays
	if err != nil {
		s.logf("server: saving volume: %v", err)
	}
	resp := volumeInfo(snd, defLocal, defRemote)
	resp["success"] = true
	writeJSON(w, http.StatusOK, resp)
}
