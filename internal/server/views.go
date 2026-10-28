package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// What the remotes show, for the launchpad (app/launchpad.go): each open
// remote, one per connection (two windows of one browser are two), reports
// the tab it shows (POST /api/remote/view) when it connects, when its tab
// changes and, at most every few seconds, when it is used. The one used last
// is the remote in use (ActiveView): the PC labels its buttons with keys and
// can switch its tab (SendLaunchpad).
//
// The PC switching the tab and the user tapping another one can cross. Each
// launchpad message carries a sequence number, and reports carry the newest
// one the remote had applied: a report older than the last message sent to
// its connection still counts as use, but its tab is the past, so the tab
// the PC sent stays. Both ends then agree on the PC's tab.

// View is what one remote shows.
type View struct {
	Conn   uint64 // the connection
	Device string
	// Tab is the tab it shows: a tab ID, or library.FavoritesKey; "" while
	// unknown.
	Tab string
	// Used is when the user last used it; zero when not since it connected.
	Used time.Time

	reported time.Time // its last report
	sent     int64     // the newest launchpad message sent to it
}

// Launchpad is the launchpad's state on one remote: while On, it shows Tab
// with each sound's key (Keys, by sound ID: "1"…"9", "0", "A"…"Z").
type Launchpad struct {
	On   bool
	Tab  string
	Keys map[uint64]string
}

type views struct {
	mu     sync.Mutex
	byConn map[uint64]*View
	seq    int64
}

// OnView, when set, is told (on its own goroutine) that the remote in use
// may show something else, or another remote may be in use: after a report,
// and when a remote that reported went away.
func (s *Server) viewChanged() {
	if f := s.OnView; f != nil {
		go f()
	}
}

// ActiveView returns the remote used last, of those connected: the one with
// the newest use, else the one that reported last. ok is false without any.
func (s *Server) ActiveView() (View, bool) {
	s.views.mu.Lock()
	defer s.views.mu.Unlock()
	var best *View
	for _, v := range s.views.byConn {
		if best == nil || v.Used.After(best.Used) ||
			v.Used.Equal(best.Used) && (v.reported.After(best.reported) || v.reported.Equal(best.reported) && v.Conn > best.Conn) {
			best = v
		}
	}
	if best == nil {
		return View{}, false
	}
	return *best, true
}

// report records a remote's report: the tab it shows, the newest launchpad
// message it had applied, and whether the user used it.
func (s *Server) report(conn uint64, device, tab string, seq int64, used bool) {
	s.views.mu.Lock()
	if s.views.byConn == nil {
		s.views.byConn = map[uint64]*View{}
	}
	v := s.views.byConn[conn]
	if v == nil {
		v = &View{Conn: conn, Device: device}
		s.views.byConn[conn] = v
	}
	now := time.Now()
	v.reported = now
	if used {
		v.Used = now
	}
	if seq >= v.sent {
		v.Tab = tab
	}
	s.views.mu.Unlock()
	s.viewChanged()
}

// dropView forgets a connection that ended.
func (s *Server) dropView(conn uint64) {
	s.views.mu.Lock()
	_, had := s.views.byConn[conn]
	delete(s.views.byConn, conn)
	s.views.mu.Unlock()
	if had {
		s.viewChanged()
	}
}

// SendLaunchpad sends a remote the launchpad's state. Its view takes the
// tab at once, so keys pressed next already meet it, and its reports from
// before the message no longer change it. It reports false when the
// connection is gone.
func (s *Server) SendLaunchpad(conn uint64, lp Launchpad) bool {
	rtc := s.currentRTC()
	if rtc == nil {
		return false
	}
	var sess *rtcSession
	for _, c := range rtc.signedIn("") {
		if c.id == conn {
			sess = c
		}
	}
	if sess == nil {
		return false
	}
	seq := s.sent(conn, lp)
	keys := map[uint64]string{}
	for id, k := range lp.Keys {
		keys[id] = k
	}
	sess.send(map[string]any{"t": "event", "e": "launchpad", "on": lp.On, "tab": lp.Tab, "seq": seq, "keys": keys})
	return true
}

// sent numbers a launchpad message to conn and gives its view the tab.
func (s *Server) sent(conn uint64, lp Launchpad) int64 {
	s.views.mu.Lock()
	defer s.views.mu.Unlock()
	s.views.seq++
	if v := s.views.byConn[conn]; v != nil {
		v.sent = s.views.seq
		if lp.On {
			v.Tab = lp.Tab
		}
	}
	return s.views.seq
}

// handleView takes a remote's report of what it shows.
func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	conn := requestConn(r)
	var body struct {
		Tab  string `json:"tab"`
		Seq  int64  `json:"seq"`
		Used bool   `json:"used"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4096)).Decode(&body); err != nil ||
		body.Tab != "" && !validLayoutKey(body.Tab) {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if conn == 0 {
		writeError(w, http.StatusBadRequest, "No connection")
		return
	}
	s.report(conn, rtcDevice(r), body.Tab, body.Seq, body.Used)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
