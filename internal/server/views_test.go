package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The remote in use is the one used last; reports that crossed a tab the PC
// sent don't take it back; a remote that goes hands over to the others.
func TestActiveView(t *testing.T) {
	s, _, _ := newTestServer(t)
	changed := make(chan struct{}, 16)
	s.OnView = func() { changed <- struct{}{} }
	active := func(conn uint64, tab string) {
		t.Helper()
		v, ok := s.ActiveView()
		if !ok || v.Conn != conn || v.Tab != tab {
			t.Fatalf("active view %+v (ok %v), want connection %d on %q", v, ok, conn, tab)
		}
	}
	if _, ok := s.ActiveView(); ok {
		t.Fatal("a view without any report")
	}
	s.report(1, "phone", "favorites", 0, true)
	s.report(2, "desk", "42", 0, false) // connected, not used
	active(1, "favorites")
	s.report(2, "desk", "42", 0, true)
	active(2, "42")

	seq := s.sent(2, Launchpad{On: true, Tab: "43"}) // the PC switched its tab
	active(2, "43")
	s.report(2, "desk", "44", seq-1, true) // tapped before the switch arrived
	active(2, "43")
	s.report(2, "desk", "44", seq, true) // tapped after
	active(2, "44")

	s.dropView(2)
	active(1, "favorites")
	s.dropView(2) // again: no change to tell

	// 5 reports, 1 drop.
	for i := range 6 {
		select {
		case <-changed:
		case <-time.After(2 * time.Second):
			t.Fatalf("OnView called %d times, want 6", i)
		}
	}
	select {
	case <-changed:
		t.Fatal("OnView called for dropping a connection twice")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHandleView(t *testing.T) {
	s, _, _ := newTestServer(t)
	device := login(t, s)
	post := func(body string, conn uint64) int {
		req := withDevice(httptest.NewRequest("POST", "/api/remote/view", strings.NewReader(body)), device)
		req = req.WithContext(context.WithValue(req.Context(), rtcConnKey{}, conn))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(`{"tab":"favorites","seq":0,"used":true}`, 7); code != http.StatusOK {
		t.Fatalf("report: %d", code)
	}
	if v, _ := s.ActiveView(); v.Conn != 7 || v.Device != device || v.Tab != "favorites" || v.Used.IsZero() {
		t.Fatalf("view %+v", v)
	}
	for _, body := range []string{`{"tab":"not a tab"}`, `{"tab":"-1"}`, `nonsense`} {
		if code := post(body, 7); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", body, code)
		}
	}
	if code := post(`{"tab":"favorites"}`, 0); code != http.StatusBadRequest {
		t.Errorf("without a connection: %d, want 400", code)
	}
}
