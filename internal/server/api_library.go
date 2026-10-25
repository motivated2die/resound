package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
	"unicode"

	"github.com/motivated2die/resound/internal/library"
)

// The phone stores colors as class-name suffixes and emoji inside a CSS
// string, so both are restricted to values that are safe there.
var (
	colorPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	layoutPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,16}$`)
)

const maxEmojiBytes = 64

func validEmoji(s string) bool {
	if len(s) > maxEmojiBytes {
		return false
	}
	for _, r := range s {
		if r == '"' || r == '\\' || r == '<' || r == '>' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// validLayoutKey accepts a tab ID or the favorites key.
func validLayoutKey(k string) bool {
	if k == library.FavoritesKey {
		return true
	}
	id, err := strconv.ParseUint(k, 10, 64)
	return err == nil && id > 0
}

func newEpoch() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// libraryETag changes whenever the library changes, including across restarts.
func (s *Server) libraryETag() string {
	return fmt.Sprintf(`"%s-%d"`, s.epoch, s.lib.Revision())
}

// handleLibrary returns tabs, sounds, settings and layouts in one response.
// Clients poll it with If-None-Match and get 304 while nothing changed.
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	s.lib.Rescan(5 * time.Second)
	etag := s.libraryETag()
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	defLocal, defRemote := s.player.DefaultVolumes()
	tabs := []map[string]any{}
	sounds := []map[string]any{}
	for _, t := range s.lib.PhoneTabs() {
		ids := make([]uint64, 0, len(t.Sounds))
		for _, snd := range t.Sounds {
			ids = append(ids, snd.ID)
			j := soundJSON(snd, defLocal, defRemote)
			j["tabId"] = snd.TabID
			sounds = append(sounds, j)
		}
		tabs = append(tabs, map[string]any{"id": t.ID, "name": t.Name, "path": t.Path, "sounds": ids})
	}
	layouts := map[string]any{}
	for k, lay := range s.lib.Layouts() {
		layouts[k] = layoutJSON(lay)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"revision": s.lib.Revision(),
		"tabs":     tabs,
		"sounds":   sounds,
		"layouts":  layouts,
	})
}

func layoutJSON(l library.Layout) map[string]any {
	orders := map[string][]uint64{}
	for k, v := range l.Orders {
		orders[k] = v
	}
	return map[string]any{"mode": l.Mode, "sort": l.Sort, "orders": orders}
}

// appearanceBody: a field that is absent stays as it is; "" or null clears it.
type appearanceBody struct {
	Color *string `json:"color"`
	Emoji *string `json:"emoji"`
}

func (b appearanceBody) validate() error {
	if b.Color != nil && *b.Color != "" && *b.Color != "default" && !colorPattern.MatchString(*b.Color) {
		return fmt.Errorf("invalid color")
	}
	if b.Emoji != nil && !validEmoji(*b.Emoji) {
		return fmt.Errorf("invalid emoji")
	}
	return nil
}

func (b appearanceBody) apply(st *library.SoundSettings) {
	if b.Color != nil {
		st.Color = *b.Color
		if st.Color == "default" {
			st.Color = ""
		}
	}
	if b.Emoji != nil {
		st.Emoji = *b.Emoji
	}
}

func (s *Server) handleAppearance(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body appearanceBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := body.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snd, found, err := s.lib.Update(id, body.apply)
	if !found {
		writeError(w, http.StatusNotFound, "Sound not found")
		return
	}
	if err != nil {
		s.logf("server: saving appearance: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "color": snd.Color, "emoji": snd.Emoji})
}

// layoutBody replaces a layout: its mode, sorting and custom orders. Without
// "sort" (an app from before sorting) the sorting stays as it is.
type layoutBody struct {
	Mode   string              `json:"mode"`
	Sort   *string             `json:"sort"`
	Orders map[string][]uint64 `json:"orders"`
}

func (b layoutBody) layout() (library.Layout, error) {
	if b.Mode != "" && !layoutPattern.MatchString(b.Mode) {
		return library.Layout{}, fmt.Errorf("invalid layout mode")
	}
	out := library.Layout{Mode: b.Mode}
	if b.Sort != nil {
		// Like the mode, any short name: a newer app's sort mode is kept.
		if *b.Sort != "" && !layoutPattern.MatchString(*b.Sort) {
			return library.Layout{}, fmt.Errorf("invalid sort mode")
		}
		out.Sort = *b.Sort
	}
	if len(b.Orders) > 8 {
		return library.Layout{}, fmt.Errorf("too many orders")
	}
	for mode, ids := range b.Orders {
		if !layoutPattern.MatchString(mode) || len(ids) > 10000 {
			return library.Layout{}, fmt.Errorf("invalid order")
		}
		if len(ids) > 0 {
			if out.Orders == nil {
				out.Orders = map[string][]uint64{}
			}
			out.Orders[mode] = ids
		}
	}
	return out, nil
}

func (s *Server) handleSetLayout(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !validLayoutKey(key) {
		writeError(w, http.StatusBadRequest, "Invalid layout key")
		return
	}
	var body layoutBody
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	lay, err := body.layout()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.lib.UpdateLayout(key, func(l *library.Layout) {
		if body.Sort == nil {
			lay.Sort = l.Sort
		}
		*l = lay
	})
	if err != nil {
		s.logf("server: saving layout: %v", err)
	}
	resp := layoutJSON(saved)
	resp["success"] = true
	writeJSON(w, http.StatusOK, resp)
}

// handleTabOrder stores the phone's arrangement of its tabs (edit mode:
// long press and drag a tab) and tells the other phones. The answer is the
// order the phone shows now, with tabs it hadn't seen yet placed.
func (s *Server) handleTabOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order []uint64 `json:"order"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&body); err != nil || len(body.Order) > 10000 {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	order, err := s.lib.SetTabOrder(body.Order)
	if err != nil {
		s.logf("server: saving the tab order: %v", err)
	}
	s.LibraryChanged()
	if order == nil {
		order = []uint64{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "order": order})
}

// importBody carries phone settings to adopt: from an exported settings
// file, or from a phone that stored them locally (earlier versions keyed
// them by file path, so sounds and orders may name paths instead of IDs).
type importBody struct {
	// Overwrite replaces values that are already set; otherwise only empty
	// ones are filled, so a second phone never undoes the first one's choices.
	Overwrite bool `json:"overwrite"`
	Sounds    []struct {
		ID    uint64  `json:"id"`
		Path  string  `json:"path"`
		Color *string `json:"color"`
		Emoji *string `json:"emoji"`
	} `json:"sounds"`
	Layouts map[string]struct {
		Mode   string                       `json:"mode"`
		Orders map[string][]json.RawMessage `json:"orders"` // IDs or paths
	} `json:"layouts"`
}

func (s *Server) resolveSound(id uint64, path string) (uint64, bool) {
	if id != 0 {
		if _, _, ok := s.lib.Sound(id); ok {
			return id, true
		}
	}
	if path != "" {
		return s.lib.FindPath(path)
	}
	return 0, false
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var body importBody
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	sounds, layouts, skipped := 0, 0, 0
	for _, in := range body.Sounds {
		id, ok := s.resolveSound(in.ID, in.Path)
		ap := appearanceBody{Color: in.Color, Emoji: in.Emoji}
		if !ok || ap.validate() != nil {
			skipped++
			continue
		}
		s.lib.Update(id, func(st *library.SoundSettings) {
			if !body.Overwrite {
				if st.Color != "" {
					ap.Color = nil
				}
				if st.Emoji != "" {
					ap.Emoji = nil
				}
			}
			ap.apply(st)
		})
		sounds++
	}
	for key, in := range body.Layouts {
		if !validLayoutKey(key) {
			skipped++
			continue
		}
		lb := layoutBody{Mode: in.Mode, Orders: map[string][]uint64{}}
		for mode, entries := range in.Orders {
			var ids []uint64
			for _, raw := range entries {
				var id uint64
				var path string
				if json.Unmarshal(raw, &id) != nil {
					if json.Unmarshal(raw, &path) != nil {
						continue
					}
					if n, err := strconv.ParseUint(path, 10, 64); err == nil {
						id, path = n, ""
					}
				}
				if sid, ok := s.resolveSound(id, path); ok {
					ids = append(ids, sid)
				}
			}
			lb.Orders[mode] = ids
		}
		lay, err := lb.layout()
		if err != nil {
			skipped++
			continue
		}
		s.lib.UpdateLayout(key, func(cur *library.Layout) {
			if body.Overwrite || cur.Mode == "" {
				if lay.Mode != "" {
					cur.Mode = lay.Mode
				}
			}
			for mode, ids := range lay.Orders {
				if cur.Orders == nil {
					cur.Orders = map[string][]uint64{}
				}
				if body.Overwrite || len(cur.Orders[mode]) == 0 {
					cur.Orders[mode] = ids
				}
			}
		})
		layouts++
	}
	s.logf("server: imported phone settings for %d sound(s) and %d layout(s), %d skipped", sounds, layouts, skipped)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "sounds": sounds, "layouts": layouts, "skipped": skipped})
}

func (s *Server) handleResetAppearance(w http.ResponseWriter, r *http.Request) {
	if err := s.lib.ResetAppearance(); err != nil {
		s.logf("server: resetting appearance: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
