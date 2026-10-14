package server

// The app's connection. The app is a static web page on an HTTPS host
// (webapp/); it opens a WebRTC data channel straight to the PC, without a
// signaling server (the technique of libp2p's "WebRTC Direct"):
//
//   - The QR code gives the app this PC's addresses, the UDP port and the
//     SHA-256 fingerprint of a long-lived DTLS certificate.
//   - The app creates its offer as usual and writes the PC's answer itself:
//     ICE-lite, the candidates from the QR code, the fingerprint, and ICE
//     credentials it picks: ufrag "d"+device ID+nonce with the device key as
//     password, or "p"+nonce with the pairing code's password (pairPassword).
//   - The PC sees the first STUN check, verifies its MESSAGE-INTEGRITY with
//     that password, and only then sets up a peer connection with the same
//     credentials and a made-up offer carrying the app's ufrag.
//   - DTLS authenticates the PC to the app (the fingerprint). The app
//     authenticates itself with its first message (hello or pair).
//
// Messages are JSON text. A message longer than rtcChunk is split into frames
// that start with '+'; the last frame of a message starts with '.'. Requests
// ({"t":"req"}) are HTTP requests in a message, served by Handler, so the API
// is the REST API the remote was written for. Events ({"t":"event","e":…})
// go the other way, unasked, to every signed-in app (broadcast), e.g. when
// the library changed; apps that don't know an event ignore it.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/stun/v4"
	"github.com/pion/webrtc/v4"
)

const (
	rtcChunk       = 16 << 10 // bytes per frame; browsers accept 64 KiB messages at least
	rtcMaxMessage  = 4 << 20  // a reassembled message from the app
	rtcMaxSessions = 16
	rtcSetupWait   = 20 * time.Second // from the first check to the app's first message
	rtcUfragDevice = 'd'
	rtcUfragPair   = 'p'
	rtcDeviceIDLen = 12 // hex characters (randomHex(6))
)

// rtcProtocol is the app protocol version, sent in the welcome message.
const rtcProtocol = 1

type rtcDeviceKey struct{}

// rtcDevice returns the app device a request came from, or "".
func rtcDevice(r *http.Request) string {
	id, _ := r.Context().Value(rtcDeviceKey{}).(string)
	return id
}

// RTC accepts the app's WebRTC connections on a UDP port.
type RTC struct {
	s           *Server
	cert        webrtc.Certificate
	fingerprint []byte
	loopback    bool // tests connect over 127.0.0.1

	mu       sync.Mutex
	conn     *net.UDPConn
	mux      *ice.UDPMuxDefault
	sessions map[string]*rtcSession // by local ufrag
	closed   bool
}

func newRTC(s *Server) (*RTC, error) {
	certPEM, err := s.auth.rtcCertPEM(newRTCCertPEM)
	if err != nil {
		return nil, err
	}
	cert, err := parseRTCCert(certPEM)
	if err != nil {
		return nil, err
	}
	fp := sha256.Sum256(cert.x509.Raw)
	return &RTC{s: s, cert: webrtc.CertificateFromX509(cert.key, cert.x509), fingerprint: fp[:], sessions: map[string]*rtcSession{}}, nil
}

// Fingerprint is the certificate's SHA-256 fingerprint, base64url, as the
// QR code carries it.
func (r *RTC) Fingerprint() string { return base64.RawURLEncoding.EncodeToString(r.fingerprint) }

// listen opens the UDP port (all interfaces).
func (r *RTC) listen(port int) error {
	r.close()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	if err != nil {
		return err
	}
	if r.loopback {
		_ = conn.Close()
		if conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}); err != nil {
			return err
		}
	}
	lf := logging.NewDefaultLoggerFactory()
	lf.DefaultLogLevel = logging.LogLevelError
	mux := ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: &rtcConn{conn: conn, r: r}, Logger: lf.NewLogger("ice")})
	r.mu.Lock()
	r.conn, r.mux, r.closed = conn, mux, false
	r.mu.Unlock()
	return nil
}

func (r *RTC) close() {
	r.mu.Lock()
	mux, sessions := r.mux, r.sessions
	r.mux, r.conn, r.sessions, r.closed = nil, nil, map[string]*rtcSession{}, true
	r.mu.Unlock()
	for _, s := range sessions {
		s.close()
	}
	if mux != nil {
		_ = mux.Close()
	}
}

// Port returns the UDP port, or 0 when not listening.
func (r *RTC) Port() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return 0
	}
	return r.conn.LocalAddr().(*net.UDPAddr).Port
}

// rtcConn hands the socket to pion's mux. Reading, it spots the first
// check of a new connection and sets the connection up before the mux looks
// for it. It must not embed *net.UDPConn: the mux would read past it.
type rtcConn struct {
	conn *net.UDPConn
	r    *RTC
}

func (c *rtcConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.conn.ReadFrom(p)
	if err == nil {
		c.r.inspect(p[:n], addr)
	}
	return n, addr, err
}
func (c *rtcConn) WriteTo(p []byte, addr net.Addr) (int, error) { return c.conn.WriteTo(p, addr) }
func (c *rtcConn) Close() error                                 { return c.conn.Close() }
func (c *rtcConn) LocalAddr() net.Addr                          { return c.conn.LocalAddr() }
func (c *rtcConn) SetDeadline(t time.Time) error                { return c.conn.SetDeadline(t) }
func (c *rtcConn) SetReadDeadline(t time.Time) error            { return c.conn.SetReadDeadline(t) }
func (c *rtcConn) SetWriteDeadline(t time.Time) error           { return c.conn.SetWriteDeadline(t) }

// inspect sets up a session for an authenticated STUN check whose ufrag has
// none yet. Everything else passes untouched (the mux drops what it doesn't
// know).
func (r *RTC) inspect(pkt []byte, addr net.Addr) {
	if !stun.IsMessage(pkt) {
		return
	}
	msg := &stun.Message{Raw: append([]byte(nil), pkt...)}
	if msg.Decode() != nil || msg.Type != stun.BindingRequest {
		return
	}
	var user stun.Username
	if user.GetFrom(msg) != nil {
		return
	}
	local, remote, ok := strings.Cut(user.String(), ":")
	if !ok || local == "" || remote == "" {
		return
	}
	r.mu.Lock()
	_, known := r.sessions[local]
	full := len(r.sessions) >= rtcMaxSessions
	r.mu.Unlock()
	if known {
		return
	}
	pwd, deviceID, ok := r.password(local)
	if !ok || stun.NewShortTermIntegrity(pwd).Check(msg) != nil {
		return // not ours, or a wrong or outdated key
	}
	if full {
		r.s.logf("app: too many connections, ignoring %s", addr)
		return
	}
	if err := r.newSession(local, remote, pwd, deviceID, addr); err != nil {
		r.s.logf("app: setting up the connection from %s: %v", addr, err)
	}
}

// password returns the ICE password for a ufrag the app picked: a device's
// key, or the current pairing code's password.
func (r *RTC) password(ufrag string) (pwd, deviceID string, ok bool) {
	switch {
	case len(ufrag) > 1+rtcDeviceIDLen && ufrag[0] == rtcUfragDevice:
		deviceID = ufrag[1 : 1+rtcDeviceIDLen]
		pwd, ok = r.s.auth.DeviceKey(deviceID)
		return pwd, deviceID, ok
	case len(ufrag) > 8 && ufrag[0] == rtcUfragPair:
		pwd, ok = r.s.auth.PairPassword()
		return pwd, "", ok
	}
	return "", "", false
}

// rtcSession is one app connection.
type rtcSession struct {
	r      *RTC
	ufrag  string
	pc     *webrtc.PeerConnection
	dc     *webrtc.DataChannel
	addr   string
	pairOK bool   // the ufrag proved the pairing code: "pair" is allowed
	expect string // the ufrag proved this device's key: "hello" is allowed

	sendMu sync.Mutex
	inbuf  bytes.Buffer // frames of the message being received

	mu     sync.Mutex
	device string // set once authenticated
	ctx    context.Context
	cancel context.CancelFunc
}

func (r *RTC) newSession(local, remote, pwd, deviceID string, addr net.Addr) error {
	r.mu.Lock()
	mux := r.mux
	r.mu.Unlock()
	if mux == nil {
		return errors.New("not listening")
	}
	var se webrtc.SettingEngine
	se.SetLite(true)
	se.SetICEUDPMux(mux)
	se.SetICECredentials(local, pwd)
	// The app's certificate is unknown (its offer never reaches the PC);
	// the app authenticates with its first message instead.
	se.DisableCertificateFingerprintVerification(true)
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	se.SetICETimeouts(5*time.Second, 15*time.Second, 2*time.Second)
	se.SetIncludeLoopbackCandidate(r.loopback)
	if err := se.SetAnsweringDTLSRole(webrtc.DTLSRoleServer); err != nil {
		return err
	}
	lf := logging.NewDefaultLoggerFactory()
	lf.DefaultLogLevel = logging.LogLevelError
	se.LoggerFactory = lf

	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{Certificates: []webrtc.Certificate{r.cert}})
	if err != nil {
		return err
	}
	negotiated, id := true, uint16(0)
	dc, err := pc.CreateDataChannel("resound", &webrtc.DataChannelInit{Negotiated: &negotiated, ID: &id})
	if err != nil {
		_ = pc.Close()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess := &rtcSession{r: r, ufrag: local, pc: pc, dc: dc, addr: addr.String(),
		pairOK: deviceID == "", expect: deviceID, ctx: ctx, cancel: cancel}

	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		if st == webrtc.PeerConnectionStateFailed || st == webrtc.PeerConnectionStateClosed {
			sess.close()
		}
	})
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		if m.IsString {
			sess.receive(m.Data)
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: rtcRemoteOffer(remote)}); err != nil {
		sess.close()
		return err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		sess.close()
		return err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		sess.close()
		return err
	}
	// Gathering registers the ufrag with the mux, which then finds it for
	// the very check that started all this.
	select {
	case <-gathered:
	case <-time.After(3 * time.Second):
		sess.close()
		return errors.New("gathering timed out")
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		sess.close()
		return errors.New("closed")
	}
	r.sessions[local] = sess
	r.mu.Unlock()

	time.AfterFunc(rtcSetupWait, func() {
		sess.mu.Lock()
		authed := sess.device != ""
		sess.mu.Unlock()
		if !authed {
			sess.close()
		}
	})
	return nil
}

// rtcRemoteOffer stands in for the app's offer, which the PC never sees.
// Only the ufrag matters; the password is unused by an ICE-lite agent and
// the fingerprint isn't checked.
func rtcRemoteOffer(remoteUfrag string) string {
	fp := strings.TrimSuffix(strings.Repeat("00:", 32), ":")
	return strings.Join([]string{
		"v=0",
		"o=- 0 0 IN IP4 0.0.0.0",
		"s=-",
		"t=0 0",
		"a=group:BUNDLE 0",
		"m=application 9 UDP/DTLS/SCTP webrtc-datachannel",
		"c=IN IP4 0.0.0.0",
		"a=mid:0",
		"a=ice-ufrag:" + remoteUfrag,
		"a=ice-pwd:unusedunusedunusedunused",
		"a=fingerprint:sha-256 " + fp,
		"a=setup:actpass",
		"a=sctp-port:5000",
		"a=max-message-size:262144",
		"",
	}, "\r\n")
}

func (s *rtcSession) close() {
	s.cancel()
	s.r.mu.Lock()
	if s.r.sessions[s.ufrag] == s {
		delete(s.r.sessions, s.ufrag)
	}
	s.r.mu.Unlock()
	// Not on this goroutine: it may be a pion callback. A last message (such
	// as "denied") is still queued; closing at once would drop it.
	go func() {
		deadline := time.Now().Add(time.Second)
		for s.dc.BufferedAmount() > 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		_ = s.pc.Close()
	}()
}

// receive collects frames into messages. Pion calls it from one goroutine.
func (s *rtcSession) receive(frame []byte) {
	if len(frame) == 0 || s.inbuf.Len()+len(frame) > rtcMaxMessage {
		s.close()
		return
	}
	s.inbuf.Write(frame[1:])
	if frame[0] == '+' {
		return
	}
	msg := bytes.Clone(s.inbuf.Bytes())
	s.inbuf.Reset()
	s.handle(msg)
}

// broadcast sends a message to every signed-in app connection and returns
// how many got it. Connections still pairing or saying hello get nothing.
func (r *RTC) broadcast(v any) int {
	r.mu.Lock()
	sessions := make([]*rtcSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()
	n := 0
	for _, s := range sessions {
		s.mu.Lock()
		signedIn := s.device != ""
		s.mu.Unlock()
		if signedIn {
			s.send(v)
			n++
		}
	}
	return n
}

// send writes one message, split into frames.
func (s *rtcSession) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	for {
		n := len(data)
		prefix := "."
		if n > rtcChunk {
			n = rtcChunk
			for n > 0 && !utf8.RuneStart(data[n]) {
				n--
			}
			prefix = "+"
		}
		if err := s.dc.SendText(prefix + string(data[:n])); err != nil {
			return
		}
		data = data[n:]
		if prefix == "." {
			return
		}
	}
}

type rtcMessage struct {
	T string `json:"t"`
	// hello / pair
	Key   string `json:"key,omitempty"`
	Code  string `json:"code,omitempty"`
	Agent string `json:"agent,omitempty"`
	// req
	ID      int64             `json:"id,omitempty"`
	Method  string            `json:"m,omitempty"`
	Path    string            `json:"p,omitempty"`
	Headers map[string]string `json:"h,omitempty"`
	Body    string            `json:"b,omitempty"`
}

func (s *rtcSession) handle(raw []byte) {
	var m rtcMessage
	if json.Unmarshal(raw, &m) != nil {
		s.close()
		return
	}
	s.mu.Lock()
	device := s.device
	s.mu.Unlock()
	srv := s.r.s

	switch {
	case m.T == "hello" && device == "" && s.expect != "":
		key, ok := srv.auth.DeviceKey(s.expect)
		if !ok || subtle.ConstantTimeCompare([]byte(m.Key), []byte(key)) != 1 {
			s.send(map[string]any{"t": "denied"})
			s.close()
			return
		}
		s.setDevice(s.expect)
		srv.logf("app: device %s connected from %s", s.expect, s.addr)
		s.send(map[string]any{"t": "welcome", "v": rtcProtocol, "name": srv.Name})

	case m.T == "pair" && device == "" && s.pairOK:
		id, key, err := srv.auth.PairDevice(m.Code, m.Agent)
		if err != nil {
			srv.logf("app: invalid or used pairing code from %s", s.addr)
			s.send(map[string]any{"t": "denied"})
			s.close()
			return
		}
		s.setDevice(id)
		srv.logf("app: device %s paired from %s", id, s.addr)
		s.send(map[string]any{"t": "welcome", "v": rtcProtocol, "name": srv.Name, "device": id, "key": key})

	case m.T == "req" && device != "":
		go s.serve(m, device)

	case m.T == "ping":
		s.send(map[string]any{"t": "pong"})

	default:
		if device == "" {
			s.send(map[string]any{"t": "denied"})
		}
		s.close()
	}
}

func (s *rtcSession) setDevice(id string) {
	s.mu.Lock()
	s.device = id
	s.mu.Unlock()
}

// serve runs an API request through the HTTP handler.
func (s *rtcSession) serve(m rtcMessage, device string) {
	reply := map[string]any{"t": "res", "id": m.ID}
	if !strings.HasPrefix(m.Path, "/api/") {
		reply["s"] = http.StatusNotFound
		s.send(reply)
		return
	}
	ctx := context.WithValue(s.ctx, rtcDeviceKey{}, device)
	req, err := http.NewRequestWithContext(ctx, m.Method, m.Path, strings.NewReader(m.Body))
	if err != nil {
		reply["s"] = http.StatusBadRequest
		s.send(reply)
		return
	}
	for k, v := range m.Headers {
		req.Header.Set(k, v)
	}
	req.RemoteAddr = s.addr
	rec := &rtcRecorder{header: http.Header{}}
	s.r.s.handler().ServeHTTP(rec, req)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	reply["s"] = rec.status
	h := map[string]string{}
	for _, k := range []string{"Content-Type", "ETag"} {
		if v := rec.header.Get(k); v != "" {
			h[k] = v
		}
	}
	reply["h"] = h
	if rec.body.Len() > 0 {
		reply["b"] = rec.body.String()
	}
	s.send(reply)
}

type rtcRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *rtcRecorder) Header() http.Header { return w.header }
func (w *rtcRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(b)
}
func (w *rtcRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

// --- certificate -------------------------------------------------------------

type rtcCert struct {
	key  *ecdsa.PrivateKey
	x509 *x509.Certificate
}

// newRTCCertPEM creates the DTLS certificate. Browsers only compare its
// fingerprint, so it is self-signed and long-lived.
func newRTCCertPEM() (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Resound"},
		NotBefore:    time.Now().AddDate(0, 0, -1),
		NotAfter:     time.Now().AddDate(30, 0, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", err
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	return string(out), nil
}

func parseRTCCert(s string) (rtcCert, error) {
	var c rtcCert
	rest := []byte(s)
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		var err error
		switch b.Type {
		case "CERTIFICATE":
			c.x509, err = x509.ParseCertificate(b.Bytes)
		case "EC PRIVATE KEY":
			c.key, err = x509.ParseECPrivateKey(b.Bytes)
		}
		if err != nil {
			return c, err
		}
	}
	if c.key == nil || c.x509 == nil {
		return c, fmt.Errorf("app certificate: missing certificate or key")
	}
	return c, nil
}
