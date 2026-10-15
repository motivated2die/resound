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
// sliderPosition tells the card whether a custom volume is quieter (< 0) or
// louder than the default; defLocal is the default "You hear" volume.
func soundJSON(s library.Sound, defLocal int) map[string]any {
	return map[string]any{
		"id": s.ID, "name": s.Name, "path": s.Path, "isFavorite": s.Favorite,
		"hasCustomVolume": s.LocalVolume != nil || s.RemoteVolume != nil,
		"sliderPosition":  sliderPosition(s, defLocal),
		"color":           s.Color,
		"emoji":           s.Emoji,
		"durationMs":      s.Duration.Milliseconds(),
	}
}

// volumeInfo reproduces the Soundux Remote's per-sound volume model: one
// slider from -50 to +50 scales both defaults by 0x..2x.
func volumeInfo(s library.Sound, defLocal, defRemote int) map[string]any {
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
		"sliderPosition":      sliderPosition(s, defLocal),
		"volumeRatio":         volumeRatio(s, defLocal),
	}
}

// volumeRatio is a sound's "You hear" volume relative to the default.
func volumeRatio(s library.Sound, defLocal int) float64 {
	ratio := 1.0
	if s.LocalVolume != nil {
		switch {
		case defLocal > 0:
			ratio = float64(*s.LocalVolume) / float64(defLocal)
		case *s.LocalVolume > 0:
			ratio = 2
		}
	}
	return ratio
}

// sliderPosition is volumeRatio on the slider's scale, -50..+50 (0: the
// default).
func sliderPosition(s library.Sound, defLocal int) int {
	ratio := volumeRatio(s, defLocal)
	switch {
	case ratio > 2:
		return 50
	case ratio < 0:
		return -50
	}
	return int(math.Round((ratio - 1) * 50))
}

// --- auth ------------------------------------------------------------------

// handleLogout signs the app device out (the app forgets this PC).
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.ForgetDevice(rtcDevice(r))
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// --- library ---------------------------------------------------------------

func (s *Server) handleTabs(w http.ResponseWriter, r *http.Request) {
	s.lib.Rescan(5 * time.Second) // a reload on the phone picks up new files
	tabs := []map[string]any{}
	for _, t := range s.lib.PhoneTabs() { // in the order the phone arranged
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
	defLocal, _ := s.player.DefaultVolumes()
	sounds := []map[string]any{}
	for _, snd := range tab.Sounds {
		sounds = append(sounds, soundJSON(snd, defLocal))
	}
	writeJSON(w, http.StatusOK, sounds)
}

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	defLocal, _ := s.player.DefaultVolumes()
	sounds := []map[string]any{}
	for _, snd := range s.lib.Favorites() {
		sounds = append(sounds, soundJSON(snd, defLocal))
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
	for k, v := range soundJSON(snd, defLocal) {
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

// handlePanelToggle opens or closes the panel on the PC (a long press on the
// phone's logo).
func (s *Server) handlePanelToggle(w http.ResponseWriter, r *http.Request) {
	open := s.player.TogglePanel()
	s.logf("remote: panel %s from %s", map[bool]string{true: "opened", false: "closed"}[open], rtcDevice(r))
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "open": open})
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
