package server

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// rtcAnswer builds the PC's answer the way the app does (webapp/src/js/rtc.js).
func rtcAnswer(offer, ufrag, pwd, fingerprint string, port int) string {
	mid := "0"
	if m := regexp.MustCompile(`a=mid:(\S+)`).FindStringSubmatch(offer); m != nil {
		mid = m[1]
	}
	raw, _ := base64.RawURLEncoding.DecodeString(fingerprint)
	hexFP := strings.ToUpper(hex.EncodeToString(raw))
	var pairs []string
	for i := 0; i < len(hexFP); i += 2 {
		pairs = append(pairs, hexFP[i:i+2])
	}
	return strings.Join([]string{
		"v=0",
		"o=- 1 0 IN IP4 0.0.0.0",
		"s=-",
		"t=0 0",
		"a=ice-lite",
		"a=group:BUNDLE " + mid,
		fmt.Sprintf("m=application %d UDP/DTLS/SCTP webrtc-datachannel", port),
		"c=IN IP4 127.0.0.1",
		"a=mid:" + mid,
		"a=ice-ufrag:" + ufrag,
		"a=ice-pwd:" + pwd,
		"a=fingerprint:sha-256 " + strings.Join(pairs, ":"),
		"a=setup:passive",
		"a=sctp-port:5000",
		"a=max-message-size:262144",
		fmt.Sprintf("a=candidate:1 1 udp 2130706431 127.0.0.1 %d typ host", port),
		"a=end-of-candidates",
		"",
	}, "\r\n")
}

type rtcClient struct {
	pc   *webrtc.PeerConnection
	in   chan map[string]any
	open chan struct{}
	dc   *webrtc.DataChannel
	buf  strings.Builder
}

func dialRTC(t *testing.T, r *RTC, ufrag, pwd string) *rtcClient {
	t.Helper()
	var se webrtc.SettingEngine
	se.SetIncludeLoopbackCandidate(true)
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	negotiated, id := true, uint16(0)
	dc, err := pc.CreateDataChannel("resound", &webrtc.DataChannelInit{Negotiated: &negotiated, ID: &id})
	if err != nil {
		t.Fatal(err)
	}
	c := &rtcClient{pc: pc, dc: dc, in: make(chan map[string]any, 16), open: make(chan struct{})}
	dc.OnOpen(func() { close(c.open) })
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		c.buf.Write(m.Data[1:])
		if m.Data[0] == '+' {
			return
		}
		var v map[string]any
		_ = json.Unmarshal([]byte(c.buf.String()), &v)
		c.buf.Reset()
		c.in <- v
	})
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer := rtcAnswer(offer.SDP, ufrag, pwd, r.Fingerprint(), r.Port())
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *rtcClient) waitOpen(d time.Duration) bool {
	select {
	case <-c.open:
		return true
	case <-time.After(d):
		return false
	}
}

func (c *rtcClient) send(t *testing.T, v any) {
	data, _ := json.Marshal(v)
	if err := c.dc.SendText("." + string(data)); err != nil {
		t.Fatal(err)
	}
}

func (c *rtcClient) recv(t *testing.T) map[string]any {
	t.Helper()
	select {
	case v := <-c.in:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("no message from the PC")
		return nil
	}
}

func startTestRTC(t *testing.T, s *Server) *RTC {
	t.Helper()
	r, err := newRTC(s)
	if err != nil {
		t.Fatal(err)
	}
	r.loopback = true
	if err := r.listen(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.close)
	s.mu.Lock()
	s.rtc = r
	s.mu.Unlock()
	return r
}

func TestRTCPairAndReconnect(t *testing.T) {
	s, player, soundID := newTestServer(t)
	s.Name = "Test PC"
	r := startTestRTC(t, s)

	// Pairing: the password comes from the code in the QR code.
	code := s.auth.PairCode()
	c := dialRTC(t, r, "pabcdefgh12345678", pairPassword(code))
	if !c.waitOpen(10 * time.Second) {
		t.Fatal("data channel did not open")
	}
	c.send(t, map[string]any{"t": "pair", "code": code, "agent": "test"})
	w := c.recv(t)
	if w["t"] != "welcome" || w["name"] != "Test PC" {
		t.Fatalf("welcome = %v", w)
	}
	device, key := w["device"].(string), w["key"].(string)
	if len(device) != rtcDeviceIDLen || key == "" {
		t.Fatalf("device %q key %q", device, key)
	}
	if s.auth.DeviceCount() != 1 {
		t.Fatalf("DeviceCount = %d", s.auth.DeviceCount())
	}

	c.send(t, map[string]any{"t": "req", "id": 1, "m": "POST", "p": fmt.Sprintf("/api/sounds/%d/play", soundID)})
	res := c.recv(t)
	if res["t"] != "res" || res["id"] != 1.0 || res["s"] != 200.0 {
		t.Fatalf("play = %v", res)
	}
	if len(player.played) != 1 {
		t.Fatalf("played %v", player.played)
	}
	// Web files are not served over the channel.
	c.send(t, map[string]any{"t": "req", "id": 2, "m": "GET", "p": "/index.html"})
	if res := c.recv(t); res["s"] != 404.0 {
		t.Fatalf("index.html = %v", res)
	}
	_ = c.pc.Close()

	// The pairing code was used up.
	if _, ok := s.auth.PairPassword(); ok {
		t.Fatal("pairing code still active")
	}

	// Reconnect as the device.
	c = dialRTC(t, r, "d"+device+"nonce123", key)
	if !c.waitOpen(10 * time.Second) {
		t.Fatal("reconnect: data channel did not open")
	}
	c.send(t, map[string]any{"t": "req", "id": 3, "m": "GET", "p": "/api/library"})
	if res := c.recv(t); res["t"] != "denied" {
		t.Fatalf("request before hello: %v", res)
	}
	_ = c.pc.Close()

	c = dialRTC(t, r, "d"+device+"nonce456", key)
	if !c.waitOpen(10 * time.Second) {
		t.Fatal("reconnect: data channel did not open")
	}
	c.send(t, map[string]any{"t": "hello", "key": key})
	if w := c.recv(t); w["t"] != "welcome" || w["key"] != nil {
		t.Fatalf("hello = %v", w)
	}
	c.send(t, map[string]any{"t": "req", "id": 4, "m": "GET", "p": "/api/library"})
	res = c.recv(t)
	if res["s"] != 200.0 || !strings.Contains(res["b"].(string), "boom") {
		t.Fatalf("library = %v", res)
	}
	h, _ := res["h"].(map[string]any)
	if h["ETag"] == nil {
		t.Fatalf("library headers = %v", h)
	}
	// Logging out forgets the device.
	c.send(t, map[string]any{"t": "req", "id": 5, "m": "POST", "p": "/api/auth/logout"})
	c.recv(t)
	if _, ok := s.auth.DeviceKey(device); ok {
		t.Fatal("device still paired after logout")
	}
}

func TestRTCRejectsWrongKey(t *testing.T) {
	s, _, _ := newTestServer(t)
	r := startTestRTC(t, s)
	code := s.auth.PairCode()
	// A wrong password: the PC never answers the checks.
	c := dialRTC(t, r, "pabcdefgh12345678", pairPassword(code+"x"))
	if c.waitOpen(4 * time.Second) {
		t.Fatal("connected with a wrong pairing password")
	}
	r.mu.Lock()
	n := len(r.sessions)
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d sessions set up", n)
	}
	// An unknown device.
	c = dialRTC(t, r, "d000000000000nonce", "0123456789abcdef0123456789abcdef")
	if c.waitOpen(3 * time.Second) {
		t.Fatal("connected as an unknown device")
	}
}

func TestRTCChunks(t *testing.T) {
	s, _, _ := newTestServer(t)
	r := startTestRTC(t, s)
	code := s.auth.PairCode()
	c := dialRTC(t, r, "pabcdefgh12345678", pairPassword(code))
	if !c.waitOpen(10 * time.Second) {
		t.Fatal("data channel did not open")
	}
	// A pairing message split into frames, with a long agent string around a
	// multi-byte character at the frame border.
	agent := strings.Repeat("a", rtcChunk-1) + "é" + strings.Repeat("b", 10)
	data, _ := json.Marshal(map[string]any{"t": "pair", "code": code, "agent": agent})
	half := len(data) / 2
	_ = c.dc.SendText("+" + string(data[:half]))
	_ = c.dc.SendText("." + string(data[half:]))
	if w := c.recv(t); w["t"] != "welcome" {
		t.Fatalf("welcome = %v", w)
	}
	// A long reply comes back in frames and is put together.
	c.send(t, map[string]any{"t": "req", "id": 1, "m": "POST", "p": "/api/library/import", "b": strings.Repeat("x", 3*rtcChunk)})
	if res := c.recv(t); res["t"] != "res" {
		t.Fatalf("res = %v", res)
	}
}

// The QR code's fingerprint must survive a restart, or paired phones could
// no longer connect.
func TestRTCCertificateKept(t *testing.T) {
	dir := t.TempDir()
	auth1, err := NewAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	r1, err := newRTC(&Server{auth: auth1})
	if err != nil {
		t.Fatal(err)
	}
	auth2, err := NewAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := newRTC(&Server{auth: auth2})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Fingerprint() == "" || r1.Fingerprint() != r2.Fingerprint() {
		t.Fatalf("fingerprints %q and %q", r1.Fingerprint(), r2.Fingerprint())
	}
	// Signing out all phones keeps the certificate but changes device keys.
	id, key, err := auth2.PairDevice(auth2.PairCode(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth2.ResetAll(); err != nil {
		t.Fatal(err)
	}
	if _, ok := auth2.DeviceKey(id); ok {
		t.Fatal("device survived ResetAll")
	}
	if auth2.deviceKeyLocked(id) == key {
		t.Fatal("device secret unchanged by ResetAll")
	}
}
