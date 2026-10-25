package server

import (
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/motivated2die/resound/internal/audio"
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
// louder than the default; defLocal and defRemote are the default volumes.
func soundJSON(s library.Sound, defLocal, defRemote int) map[string]any {
	return map[string]any{
		"id": s.ID, "name": s.Name, "path": s.Path, "isFavorite": s.Favorite,
		"hasCustomVolume": s.LocalVolume != nil || s.RemoteVolume != nil,
		"sliderPosition":  sliderPosition(s, defLocal, defRemote),
		"color":           s.Color,
		"emoji":           s.Emoji,
		"durationMs":      s.Duration.Milliseconds(),
	}
}

// volumeInfo reproduces the Soundux Remote's per-sound volume model: one
// slider from -50 to +50 makes the sound silent .. twice as loud (gain) on
// both outputs, relative to their defaults (sliderGain).
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
		"sliderPosition":      sliderPosition(s, defLocal, defRemote),
		"volumeRatio":         sliderGain(sliderPosition(s, defLocal, defRemote)),
	}
}

// sliderGain is what a slider position does to a sound's gain on both
// outputs. Left of the middle it follows the volume curve (audio.Gain), so
// −25 is what 50% is to 100% and −50 is silence; right of it the gain grows
// up to twice the default's at +50.
//
// The Soundux Remote multiplied the volumes themselves, which were linear
// gains there. On Resound's curve that moved each output by a different
// amount: with "You hear" at 50% and "Others hear" at 100%, a sound turned
// down to −40 was 12 dB quieter for the user but 32 dB for others, and +50
// made it 20 dB louder for the user but 6 dB for others.
func sliderGain(pos int) float32 {
	if pos < 0 {
		return audio.Gain(100 + 2*max(-50, pos))
	}
	return 1 + float32(min(50, pos))/50
}

// scaleVolume is a default volume (percent) moved by a slider position.
func scaleVolume(def, pos int) int {
	return audio.Percent(audio.Gain(def) * sliderGain(pos))
}

// sliderPosition is the slider position a sound's custom volume was set
// with, -50..+50 (0: the default). It is read from the output whose default
// is louder: the volumes are whole percentages, which resolve the quiet end
// coarsely, and a default of 0% doesn't tell positions apart at all.
func sliderPosition(s library.Sound, defLocal, defRemote int) int {
	def, custom := defLocal, s.LocalVolume
	if custom == nil || s.RemoteVolume != nil && defRemote > defLocal {
		def, custom = defRemote, s.RemoteVolume
	}
	if custom == nil {
		return 0
	}
	gd := audio.Gain(def)
	if gd <= 0 {
		if *custom > 0 {
			return 50
		}
		return 0
	}
	ratio := audio.Gain(*custom) / gd
	if ratio >= 1 {
		return min(50, int(math.Round(float64(ratio-1)*50)))
	}
	return max(-50, int(math.Round(float64(audio.Percent(ratio)-100)/2)))
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
	defLocal, defRemote := s.player.DefaultVolumes()
	sounds := []map[string]any{}
	for _, snd := range tab.Sounds {
		sounds = append(sounds, soundJSON(snd, defLocal, defRemote))
	}
	writeJSON(w, http.StatusOK, sounds)
}

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	defLocal, defRemote := s.player.DefaultVolumes()
	sounds := []map[string]any{}
	for _, snd := range s.lib.Favorites() {
		sounds = append(sounds, soundJSON(snd, defLocal, defRemote))
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
	for k, v := range soundJSON(snd, defLocal, defRemote) {
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
	stopped := started.Replaced // the phone stops showing these at once
	if stopped == nil {
		stopped = []uint64{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"id":         id,
		"playingId":  started.PlayingID,
		"lengthInMs": started.Length.Milliseconds(),
		"stopped":    stopped,
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
	scale := func(d int) *int {
		v := scaleVolume(d, pos)
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
