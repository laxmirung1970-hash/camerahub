package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T, mode string) (*App, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	s, e := openStore(dir, mode)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.DB.Close() })
	a, e := newApp(s, dir, "http://127.0.0.1:1")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(a.stopAll)
	return a, a.handler(t.TempDir())
}
func doTest(t *testing.T, h http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.RemoteAddr = "192.0.2.1:1234"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
func setupOwner(t *testing.T, a *App, h http.Handler) *http.Cookie {
	t.Helper()
	r := doTest(t, h, "POST", "/api/setup", map[string]string{"code": a.setupCode(), "name": "Fixture owner", "password": "fixture-owner-password-123"}, nil)
	if r.Code != 201 {
		t.Fatalf("setup %d: %s", r.Code, r.Body.String())
	}
	return r.Result().Cookies()[0]
}

func TestOwnerPasswordMinimumEightCharacters(t *testing.T) {
	for _, tc := range []struct {
		name, password string
		status         int
	}{
		{"seven_ascii", "1234567", http.StatusBadRequest},
		{"four_thai_not_twelve_bytes", "กขคง", http.StatusBadRequest},
		{"seven_thai", "กขคงจฉช", http.StatusBadRequest},
		{"eight_ascii", "12345678", http.StatusCreated},
		{"eight_thai", "กขคงจฉชซ", http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, h := testApp(t, "live")
			r := doTest(t, h, "POST", "/api/setup", map[string]string{"code": a.setupCode(), "name": "Fixture owner", "password": tc.password}, nil)
			if r.Code != tc.status {
				t.Fatalf("setup status = %d, want %d", r.Code, tc.status)
			}
			if tc.status == http.StatusBadRequest {
				if a.store.setting("owner_hash") != "" {
					t.Fatal("rejected setup created an owner")
				}
				return
			}
			login := doTest(t, h, "POST", "/api/login", map[string]string{"password": tc.password}, nil)
			if login.Code != http.StatusOK || len(login.Result().Cookies()) != 1 {
				t.Fatal("eight-character owner password cannot log in")
			}
		})
	}
}

func TestAuthenticationAndProtectedMedia(t *testing.T) {
	a, h := testApp(t, "demo")
	for _, path := range []string{"/api/cameras", "/api/system", "/api/media/hls/init.mp4?id=unknown", "/api/cameras/unknown/ws"} {
		if r := doTest(t, h, "GET", path, nil, nil); r.Code != 401 {
			t.Fatalf("unprotected %s: %d", path, r.Code)
		}
	}
	bad := doTest(t, h, "POST", "/api/setup", map[string]string{"code": "wrong", "name": "Owner", "password": "long-fixture-password"}, nil)
	if bad.Code != 403 {
		t.Fatal("setup code not enforced")
	}
	cookie := setupOwner(t, a, h)
	r := doTest(t, h, "GET", "/api/cameras", nil, cookie)
	if r.Code != 200 || strings.Contains(r.Body.String(), "source") || strings.Contains(r.Body.String(), "ffmpeg:") {
		t.Fatal("camera listing leaked source")
	}
	again := doTest(t, h, "POST", "/api/setup", map[string]string{"code": "wrong", "name": "Other", "password": "another-fixture-password"}, nil)
	if again.Code != 409 {
		t.Fatal("owner replaced")
	}
	req := httptest.NewRequest("POST", "http://app.local/api/logout", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("cross origin accepted")
	}
	if r = doTest(t, h, "POST", "/api/logout", map[string]string{}, cookie); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if doTest(t, h, "GET", "/api/cameras", nil, cookie).Code != 401 {
		t.Fatal("logout did not revoke session")
	}
}
func TestModesAreIsolatedAndNoLiveDiscoveryOnStartup(t *testing.T) {
	dir := t.TempDir()
	demo, e := openStore(dir, "demo")
	if e != nil {
		t.Fatal(e)
	}
	defer demo.DB.Close()
	live, e := openStore(dir, "live")
	if e != nil {
		t.Fatal(e)
	}
	defer live.DB.Close()
	d, e := newApp(demo, dir, "http://127.0.0.1:1")
	if e != nil {
		t.Fatal(e)
	}
	l, e := newApp(live, dir, "http://127.0.0.1:1")
	if e != nil {
		t.Fatal(e)
	}
	cs, _ := d.store.cameras()
	ls, _ := l.store.cameras()
	if len(cs) != 15 || len(ls) != 0 || bytes.Equal(demo.Key, live.Key) {
		t.Fatal("demo data or keys crossed into live")
	}
	if _, e = l.syncConnections(context.Background(), ""); e == nil {
		t.Fatal("live should require explicit enabled connection")
	}
}
func TestRegistryMergeAcrossIPChangesAndEncryptedSecrets(t *testing.T) {
	a, _ := testApp(t, "live")
	conn := Connection{Account: "123456", Region: "sg"}
	raw := []byte(`[{"name":"Cloud name","url":"xiaomi://123456:sg@192.168.1.50?did=99999&model=chuangmi.camera.026c02"}]`)
	cams, e := parseXiaomiSources(raw, conn)
	if e != nil || len(cams) != 1 {
		t.Fatal(e)
	}
	c := cams[0]
	c.Name = "My room"
	c.Group = "Upstairs"
	c.Favorite = true
	c.Source.Password = "fixture-secret-value"
	if e = a.store.saveCamera(c); e != nil {
		t.Fatal(e)
	}
	raw = bytes.ReplaceAll(raw, []byte("192.168.1.50"), []byte("192.168.1.77"))
	next, _ := parseXiaomiSources(raw, conn)
	if e = a.store.mergeCamera(next[0]); e != nil {
		t.Fatal(e)
	}
	cs, _ := a.store.cameras()
	if len(cs) != 1 || cs[0].ID != c.ID || cs[0].Host != "192.168.1.77" || cs[0].Name != "My room" || !cs[0].Favorite {
		t.Fatal("merge lost identity or user settings")
	}
	var secret []byte
	_ = a.store.DB.QueryRow("SELECT secret FROM cameras WHERE id=?", c.ID).Scan(&secret)
	if bytes.Contains(secret, []byte("xiaomi://")) {
		t.Fatal("source stored in plaintext")
	}
	if e = a.store.mergeCamera(next[0]); e != nil {
		t.Fatal(e)
	}
	cs, _ = a.store.cameras()
	if len(cs) != 1 {
		t.Fatal("sync duplicated camera")
	}
}
func TestXiaomiDiscoveryAcceptsGatewayEnvelopeAndFiltersVendor(t *testing.T) {
	a, h := testApp(t, "live")
	owner := setupOwner(t, a, h)
	_ = a.store.saveConnection(Connection{ID: "fixture-cloud", Vendor: "xiaomi", Account: "123456", Region: "sg", Enabled: true})
	_ = a.store.saveConnection(Connection{ID: "fixture-tapo", Vendor: "tapo", Enabled: true})
	gatewayCalls := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalls++
		if r.URL.Path != "/api/xiaomi" || r.URL.Query().Get("region") != "sg" || r.URL.Query().Get("id") != "123456" {
			t.Error("unexpected cloud request")
		}
		_, _ = io.WriteString(w, `{"sources":[{"name":"C300 fixture","url":"xiaomi://123456:sg@192.168.1.50?did=99999&model=chuangmi.camera.072ae2"},{"name":"missing IP","url":"xiaomi://123456:sg@0.0.0.0?did=88888"},{"name":"not a camera","url":"https://example.test/"}]}`)
	}))
	defer gateway.Close()
	a.bridge.Base = gateway.URL
	for i := 0; i < 2; i++ {
		r := doTest(t, h, "POST", "/api/discover?vendor=xiaomi", map[string]string{}, owner)
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	cs, _ := a.store.cameras()
	if len(cs) != 2 || cs[0].Name != "C300 fixture" || cs[0].Status != "pending" || cs[0].Verification != "untested" || cs[0].Source.Connection != "fixture-cloud" || cs[1].ParentID != cs[0].ID || cs[1].Lens != 2 || cs[1].Source.Connection != "fixture-cloud" {
		t.Fatal("gateway envelope discovery failed or falsely verified video")
	}
	conns, _ := a.store.connections()
	for _, c := range conns {
		if c.Vendor == "tapo" && !c.LastSync.IsZero() {
			t.Fatal("scoped sync contacted Tapo")
		}
	}
	r := doTest(t, h, "POST", "/api/discover?vendor=invalid", map[string]string{}, owner)
	if r.Code != 400 || gatewayCalls != 2 {
		t.Fatal("invalid vendor forwarded")
	}
	for _, raw := range []string{`{}`, `{"sources":null}`, `{"sources":{}}`, `null`, `{"error":"not sources"}`} {
		if _, err := parseXiaomiSources([]byte(raw), Connection{}); err == nil {
			t.Fatal("invalid response accepted as empty success")
		}
	}
}

func TestDiscoveryKeepsLastVideoResultUntilSourceChanges(t *testing.T) {
	a, _ := testApp(t, "live")
	cams, err := parseXiaomiSources([]byte(`{"sources":[{"name":"Fixture","url":"xiaomi://123456:sg@192.168.1.50?did=99999&model=xiaomi.camera.c01a01"}]}`), Connection{Account: "123456", Region: "sg"})
	if err != nil || len(cams) != 1 {
		t.Fatal("fixture invalid")
	}
	for _, status := range []string{"online", "error"} {
		old := cams[0]
		old.Status = status
		old.LastSeen = time.Unix(1700000000, 0).UTC()
		old.Verification = "stream_only"
		old.Message = "Last video result"
		if err = a.store.saveCamera(old); err != nil {
			t.Fatal(err)
		}
		if err = a.store.mergeCamera(cams[0]); err != nil {
			t.Fatal(err)
		}
		merged, _ := a.store.camera(old.ID)
		if merged.Status != status || merged.Verification != "stream_only" || merged.Message != old.Message || !merged.LastSeen.Equal(old.LastSeen) {
			t.Fatal("inventory sync erased video result")
		}
		next := cams[0]
		next.Host = "192.168.1.77"
		next.Source.Low = strings.ReplaceAll(next.Source.Low, "192.168.1.50", "192.168.1.77")
		if err = a.store.mergeCamera(next); err != nil {
			t.Fatal(err)
		}
		merged, _ = a.store.camera(old.ID)
		if merged.Status != "pending" || !merged.LastSeen.Equal(old.LastSeen) {
			t.Fatal("changed IP incorrectly kept last connection status")
		}
	}
	fresh := cams[0]
	fresh.ID = "unseen-camera"
	if err = a.store.mergeCamera(fresh); err != nil {
		t.Fatal(err)
	}
	newCamera, _ := a.store.camera(fresh.ID)
	if !newCamera.LastSeen.IsZero() {
		t.Fatal("new inventory item pretended to have video contact")
	}
}

func TestOwnerCanRenameCameraAndCloudSyncKeepsAlias(t *testing.T) {
	a, h := testApp(t, "live")
	owner := setupOwner(t, a, h)
	c := Camera{ID: "alias-fixture", Vendor: "xiaomi", Ref: "99999", Name: "Cloud C300", Host: "192.168.1.50", Status: "online", Verification: "stream_only", Source: Source{Low: "xiaomi://123456:sg@192.168.1.50?did=99999&model=xiaomi.camera.c01a01"}}
	if err := a.store.saveCamera(c); err != nil {
		t.Fatal(err)
	}
	r := doTest(t, h, "PATCH", "/api/cameras/"+c.ID, map[string]string{"name": "  โรงจอดรถ · ฝั่งประตู  "}, owner)
	if r.Code != 200 {
		t.Fatal("rename failed")
	}
	var renamed Camera
	if json.Unmarshal(r.Body.Bytes(), &renamed) != nil || renamed.Name != "โรงจอดรถ · ฝั่งประตู" {
		t.Fatal("alias was not trimmed/saved")
	}
	c.Name = "Changed cloud name"
	c.Status = "pending"
	if err := a.store.mergeCamera(c); err != nil {
		t.Fatal(err)
	}
	saved, err := a.store.camera(c.ID)
	if err != nil || saved.Name != renamed.Name || saved.Source.Low != c.Source.Low || saved.Status != "online" {
		t.Fatal("sync erased alias, source or video result")
	}
	if r := doTest(t, h, "PATCH", "/api/cameras/"+c.ID, map[string]string{"name": " "}, owner); r.Code != 400 {
		t.Fatal("blank name accepted")
	}
}

func TestProbeRequiresVideoPayload(t *testing.T) {
	box := func(kind string, payload []byte) []byte {
		b := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:8], kind)
		copy(b[8:], payload)
		return b
	}
	init := append(box("ftyp", nil), box("moov", nil)...)
	fragment := append(box("moof", []byte{0, 0, 0, 1}), box("mdat", []byte{1, 2, 3, 4})...)
	for _, tc := range []struct {
		name, mime string
		messages   [][]byte
		good       bool
	}{
		{"codec_without_frames", `video/mp4; codecs="avc1.640029"`, [][]byte{init}, false},
		{"video_fragment", `video/mp4; codecs="hvc1.1.6.L153.B0"`, [][]byte{init, fragment}, true},
		{"audio_fragment", `audio/mp4; codecs="opus"`, [][]byte{init, fragment}, false},
		{"malformed_fragment", `video/mp4; codecs="avc1.640029"`, [][]byte{[]byte{0, 0, 0, 255, 'm', 'o', 'o', 'f'}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u, p, ok := r.BasicAuth()
				if !ok || u != "fixture" || p != "fixture-private" {
					http.Error(w, "unauthorized", 401)
					return
				}
				if r.URL.Path != "/api/ws" || r.URL.Query().Get("src") != "fixture-camera" {
					t.Error("wrong video probe endpoint")
				}
				ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer ws.Close()
				var offer struct{ Type, Value string }
				if ws.ReadJSON(&offer) != nil {
					return
				}
				if offer.Type != "mse" || strings.Contains(offer.Value, "opus") || strings.Contains(offer.Value, "mp4a") {
					t.Error("probe must request video only")
				}
				_ = ws.WriteJSON(map[string]string{"type": "mse", "value": tc.mime})
				for _, data := range tc.messages {
					_ = ws.WriteMessage(websocket.BinaryMessage, data)
				}
			}))
			defer server.Close()
			bridge := &Bridge{Base: strings.Replace(server.URL, "http://", "http://fixture:fixture-private@", 1)}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := bridge.probe(ctx, "fixture-camera")
			if (err == nil) != tc.good {
				t.Fatalf("video result = %v, expected good=%v", err, tc.good)
			}
		})
	}
}

func TestPairingNeedsOwnerAndTokenIsReadScoped(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	r := doTest(t, h, "POST", "/api/pair/request", map[string]string{"name": "Fixture TV"}, nil)
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	var pair map[string]string
	_ = json.Unmarshal(r.Body.Bytes(), &pair)
	if doTest(t, h, "POST", "/api/pair/approve", map[string]string{"code": pair["code"]}, nil).Code != 401 {
		t.Fatal("unprotected approval")
	}
	before := doTest(t, h, "POST", "/api/pair/poll", map[string]string{"id": pair["id"], "secret": pair["secret"]}, nil)
	if strings.Contains(before.Body.String(), "token") {
		t.Fatal("unapproved token released")
	}
	_ = doTest(t, h, "POST", "/api/pair/approve", map[string]string{"code": pair["code"]}, owner)
	after := doTest(t, h, "POST", "/api/pair/poll", map[string]string{"id": pair["id"], "secret": pair["secret"]}, nil)
	var result struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(after.Body.Bytes(), &result)
	if result.Token == "" {
		t.Fatal("missing token")
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/cameras", 200}, {"/api/system", 403}, {"/api/connections", 403}} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+result.Token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("TV ACL %s: %d", tc.path, w.Code)
		}
	}
	if doTest(t, h, "POST", "/api/pair/poll", map[string]string{"id": pair["id"], "secret": pair["secret"]}, nil).Code != 404 {
		t.Fatal("pair token delivered twice")
	}
}
func TestPTZStopsAfterLeaseAndCannotControlUnsupportedCamera(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	cs, _ := a.store.cameras()
	var motor, unsupported Camera
	for _, c := range cs {
		if c.PTZ && c.Status == "online" {
			motor = c
		}
		if !c.PTZ {
			unsupported = c
		}
	}
	r := doTest(t, h, "POST", "/api/cameras/"+motor.ID+"/ptz", map[string]string{"direction": "left"}, owner)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	time.Sleep(500 * time.Millisecond)
	_ = doTest(t, h, "POST", "/api/cameras/"+motor.ID+"/ptz", map[string]string{"direction": "right"}, owner)
	time.Sleep(400 * time.Millisecond)
	a.mu.Lock()
	alive := a.leases[motor.ID] != nil
	a.mu.Unlock()
	if !alive {
		t.Fatal("stale timer stopped renewed lease")
	}
	time.Sleep(550 * time.Millisecond)
	a.mu.Lock()
	alive = a.leases[motor.ID] != nil
	a.mu.Unlock()
	if alive {
		t.Fatal("PTZ lease did not expire")
	}
	if doTest(t, h, "POST", "/api/cameras/"+unsupported.ID+"/ptz", map[string]string{"direction": "left"}, owner).Code != 409 {
		t.Fatal("unsupported PTZ pretended success")
	}
}
func TestXiaomiChallengesAndActualReplyShape(t *testing.T) {
	a, h := testApp(t, "live")
	owner := setupOwner(t, a, h)
	authCalls := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/xiaomi/command":
			jsonOut(w, 200, map[string]string{"reply": `{"ret":0,"position":1}`, "hex": "00"})
		case r.Method == "POST":
			authCalls++
			_ = r.ParseForm()
			if r.Form.Get("username") != "" {
				jsonOut(w, 401, map[string]string{"captcha": "YQ=="})
			} else if r.Form.Get("captcha") != "" {
				jsonOut(w, 401, map[string]string{"verify_email": "f***@example.test"})
			} else {
				w.WriteHeader(200)
			}
		default:
			jsonOut(w, 200, []string{"123456"})
		}
	}))
	defer gateway.Close()
	a.bridge.Base = gateway.URL
	if doTest(t, h, "POST", "/api/xiaomi/auth", map[string]string{"verify": "123456"}, owner).Code != 409 || authCalls != 0 {
		t.Fatal("out-of-order verification forwarded")
	}
	for _, body := range []map[string]string{{"username": "fixture", "password": "fixture-pass"}, {"captcha": "answer"}, {"verify": "123456"}} {
		r := doTest(t, h, "POST", "/api/xiaomi/auth", body, owner)
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	if authCalls != 3 {
		t.Fatal("challenge flow did not complete")
	}
	cam := Camera{ID: "fixture", Source: Source{PTZProtocol: "miss-step"}}
	if e := a.move(context.Background(), cam, "left"); e != nil {
		t.Fatal("valid Xiaomi reply rejected: ", e)
	}
	if e := a.move(context.Background(), cam, "stop"); e != nil {
		t.Fatal(e)
	}
}
func TestXiaomiAuthDiagnosticsAreSafeAndEmptyAccountsFail(t *testing.T) {
	for _, tc := range []struct{ raw, expected string }{
		{`xiaomi: {"code":70016,"passToken":"do-not-expose","description":"private"}`, "70016"},
		{`Post https://private.test/?token=do-not-expose: timeout`, "หมดเวลา"},
		{`lookup private.test: no such host do-not-expose`, "DNS"},
		{`x509: private certificate do-not-expose`, "ใบรับรอง"},
		{`unexpected private reply do-not-expose`, "HTTP 500"},
	} {
		msg := xiaomiAuthFailure([]byte(tc.raw), 500)
		if !strings.Contains(msg, tc.expected) || strings.Contains(msg, "do-not-expose") || strings.Contains(msg, "private") {
			t.Fatalf("unsafe or inaccurate error: %s", msg)
		}
	}
	a, h := testApp(t, "live")
	owner := setupOwner(t, a, h)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			jsonOut(w, 200, []string{})
			return
		}
		w.WriteHeader(200)
	}))
	defer gateway.Close()
	a.bridge.Base = gateway.URL
	r := doTest(t, h, "POST", "/api/xiaomi/auth", map[string]string{"username": "fixture", "password": "fixture-password"}, owner)
	if r.Code != 502 || bytes.Contains(r.Body.Bytes(), []byte(`"step":"done"`)) {
		t.Fatal("empty account list was reported as authenticated")
	}
}

func TestEncryptedBackupAndTamperDetection(t *testing.T) {
	raw := []byte(`{"mode":"live","secret":"fixture-only"}`)
	sealed, e := backupEncrypt("fixture-backup-password", raw)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(sealed, []byte("fixture-only")) {
		t.Fatal("backup not encrypted")
	}
	if _, e = backupDecrypt("wrong", sealed); e == nil {
		t.Fatal("wrong password accepted")
	}
	plain, e := backupDecrypt("fixture-backup-password", sealed)
	if e != nil || !bytes.Equal(plain, raw) {
		t.Fatal("backup roundtrip")
	}
	sealed[len(sealed)-1] ^= 1
	if _, e = backupDecrypt("fixture-backup-password", sealed); e == nil {
		t.Fatal("tampered backup accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestONVIFUsernameTokenUsesStandardNonceEncoding(t *testing.T) {
	password := "fixture-camera-password"
	o := &ONVIF{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Fatal("unexpected duplicate HTTP authentication")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		node, err := parseXML(raw)
		if err != nil {
			t.Fatal(err)
		}
		nonces := node.find("Nonce")
		if len(nonces) != 1 || nonces[0].attr("EncodingType") != "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary" {
			t.Fatal("nonce uses the wrong WS-Security encoding URI")
		}
		nonce, err := base64.StdEncoding.DecodeString(nonces[0].Text)
		if err != nil || len(nonce) < 16 {
			t.Fatal("invalid nonce")
		}
		if len(nonce) != 16 || strings.IndexFunc(string(nonce), func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
			t.Fatal("nonce is not compatible printable text")
		}
		if r.Header.Get("Content-Type") != "application/soap+xml;charset=utf-8" || strings.Contains(string(raw), "mustUnderstand") {
			t.Fatal("unexpected SOAP compatibility headers")
		}
		created := node.first("Created")
		if _, err = time.Parse(time.RFC3339Nano, created); err != nil {
			t.Fatal("invalid creation time")
		}
		input := append(append(append([]byte{}, nonce...), []byte(created)...), []byte(password)...)
		digest := sha1.Sum(input)
		if node.first("Password") != base64.StdEncoding.EncodeToString(digest[:]) {
			t.Fatal("password digest does not match nonce and creation time")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("<Envelope><Body><Success/></Body></Envelope>")), Header: http.Header{}}, nil
	})}}
	if _, err := o.call(context.Background(), "http://192.168.1.60:2020/onvif/device_service", "fixture", password, "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation", "<tds:GetDeviceInformation/>"); err != nil {
		t.Fatal(err)
	}
}

func TestONVIFSetupErrorsAreSpecificAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		auth   bool
	}{
		{401, "secret-camera-response", true},
		{500, "<Envelope><Fault><Code>ter:NotAuthorized</Code><Reason>secret-camera-response</Reason></Fault></Envelope>", true},
		{400, "secret-camera-response", false},
	} {
		o := &ONVIF{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
		})}}
		_, err := o.inspect(context.Background(), "192.168.1.60", "fixture-user", "secret-camera-password")
		if err == nil {
			t.Fatal("rejected response accepted")
		}
		message := onvifSetupMessage(err)
		if strings.Contains(message, "secret-") || strings.Contains(message, "fixture-user") {
			t.Fatal("camera response or credentials leaked")
		}
		if strings.Contains(message, "ปฏิเสธการยืนยันบัญชี") != tc.auth {
			t.Fatal("wrong authentication diagnosis")
		}
		if !tc.auth && !strings.Contains(message, "GetDeviceInformation") {
			t.Fatal("operation diagnostic missing")
		}
	}
}

func TestONVIFProfileDiscoveryAndBoundedMovement(t *testing.T) {
	calls := []string{}
	o := &ONVIF{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		calls = append(calls, s)
		if !strings.Contains(s, "PasswordDigest") {
			t.Fatal("missing WSSE digest")
		}
		out := ""
		switch {
		case strings.Contains(s, "GetDeviceInformation"):
			out = `<Model>C200</Model><SerialNumber>CAM12345</SerialNumber>`
		case strings.Contains(s, "GetCapabilities"):
			out = `<Capabilities><Media><XAddr>http://192.168.1.60:2020/media</XAddr></Media><PTZ><XAddr>http://192.168.1.60:2020/ptz</XAddr></PTZ></Capabilities>`
		case strings.Contains(s, "GetProfiles"):
			out = `<Profiles token="extra"/><Profiles token="main"><PTZConfiguration/><AudioEncoderConfiguration/></Profiles><Profiles token="sub"/><Profiles token="unused"/>`
		case strings.Contains(s, "GetStreamUri"):
			if strings.Contains(s, ">unused<") {
				t.Fatal("requested an auxiliary profile after finding main/sub streams")
			}
			stream := "stream1"
			if strings.Contains(s, ">sub<") {
				stream = "stream2"
			}
			if strings.Contains(s, ">extra<") {
				stream = "stream8"
			}
			out = `<MediaUri><Uri>rtsp://192.168.1.60:554/` + stream + `</Uri></MediaUri>`
		default:
			out = `<Success/>`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<Envelope><Body>` + out + `</Body></Envelope>`)), Header: http.Header{}}, nil
	})}}
	c, e := o.inspect(context.Background(), "192.168.1.60", "fixture", "fixture-password")
	if e != nil {
		t.Fatal(e)
	}
	if !c.PTZ || !c.Audio || !strings.Contains(c.Source.Low, "stream2") || !strings.Contains(c.Source.High, "stream1") {
		t.Fatal("wrong profiles or capabilities")
	}
	if c.Source.Profile != "main" || strings.Contains(c.Source.Low, "stream8") {
		t.Fatal("selected an auxiliary profile for viewing or PTZ")
	}
	if e = o.move(context.Background(), c, "left"); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(calls[len(calls)-1], "<tptz:Timeout>PT1S</tptz:Timeout>") {
		t.Fatal("unbounded motor command")
	}
	if e = o.move(context.Background(), c, "stop"); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(calls[len(calls)-1], "<tptz:Stop>") {
		t.Fatal("missing stop command")
	}
	if strings.Contains(calls[len(calls)-1], "<tptz:Zoom>true") {
		t.Fatal("pan/tilt-only camera must not require a zoom motor to stop")
	}
	if sameCameraURL("http://169.254.169.254/latest/meta-data", c.Host) {
		t.Fatal("accepted different host service")
	}
}
func TestHLSPathsStayAuthenticatedAndMalformedJSONRejected(t *testing.T) {
	p := rewritePlaylist("#EXTM3U\nhls/playlist.m3u8?id=abc\n#EXT-X-MAP:URI=\"init.mp4?id=abc\"\nsegment.m4s?id=abc&n=1\n")
	if !strings.Contains(p, "/api/media/hls/playlist.m3u8") || !strings.Contains(p, `URI="/api/media/hls/init.mp4`) {
		t.Fatal(p)
	}
	a, h := testApp(t, "demo")
	cookie := setupOwner(t, a, h)
	req := httptest.NewRequest("POST", "/api/discover", strings.NewReader(`{}junk`))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req) // discover has no body: validate the decoder separately instead
	req = httptest.NewRequest("POST", "/test", strings.NewReader(`{}junk`))
	w = httptest.NewRecorder()
	var v struct{}
	if readJSON(w, req, &v) || w.Code != 400 {
		t.Fatal("malformed trailing JSON accepted")
	}
}

func TestRTSPCredentialProbeIsTransientAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, want string
	}{
		{200, `{}`, "accepted"},
		{500, `rtsp://fixture:secret-password@192.168.1.60:554/stream1: 401 Unauthorized`, "rejected"},
		{500, `streams: rtsp: wrong user/pass`, "rejected"},
		{500, `secret-password: unsupported codec`, "unavailable"},
	} {
		deleted := false
		name := ""
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "PATCH":
				name = r.URL.Query().Get("name")
				if !strings.HasPrefix(name, "_tapo_check_") || r.URL.Query().Get("src") != "rtsp://fixture:secret-password@192.168.1.60:554/stream1" {
					t.Error("wrong transient stream")
				}
			case "GET":
				if r.URL.Query().Get("src") != name {
					t.Error("wrong probe target")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			case "DELETE":
				deleted = r.URL.Query().Get("src") == name
			default:
				t.Error("probe must not use persistent PUT registration")
			}
		}))
		bridge := &Bridge{Base: server.URL, Client: server.Client()}
		got := bridge.checkRTSP(context.Background(), "rtsp://fixture:secret-password@192.168.1.60:554/stream1")
		server.Close()
		if got != tc.want || !deleted || strings.Contains(got, "secret") {
			t.Fatal("wrong probe result or missing cleanup")
		}
	}
}

func TestDemoStreamsShareOneLoopProducer(t *testing.T) {
	sources := []string{}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			sources = append(sources, r.URL.Query().Get("src"))
		}
		w.WriteHeader(200)
	}))
	defer gateway.Close()
	b := &Bridge{Base: gateway.URL, Client: gateway.Client()}
	for _, id := range []string{"camera-a", "camera-b"} {
		c := Camera{ID: id, Verification: "demo", Source: Source{Low: "ffmpeg:/media/scene-1-low.mp4#input=file#video=copy#audio=copy"}}
		if err := b.ensure(context.Background(), c, "low"); err != nil {
			t.Fatal(err)
		}
	}
	if len(sources) != 3 || !strings.Contains(sources[0], "#input=demo_loop") || !strings.HasPrefix(sources[1], "rtsp://127.0.0.1:8554/_demo_") || sources[1] != sources[2] {
		t.Fatal("demo aliases should consume one shared looping producer")
	}
}

func TestMediaErrorsDoNotExposeSourceCredentials(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "gateway-fixture" || password != "gateway-private" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/api/ws" {
			conn, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if e != nil {
				return
			}
			defer conn.Close()
			_ = conn.WriteJSON(map[string]string{"type": "error", "value": "rtsp://fixture-user:fixture-secret@192.168.1.5/stream"})
			_, _, _ = conn.ReadMessage()
			return
		}
		w.WriteHeader(200)
	}))
	defer gateway.Close()
	a.bridge.Base = strings.Replace(gateway.URL, "http://", "http://gateway-fixture:gateway-private@", 1)
	a.bridge.Client = gateway.Client()
	appServer := httptest.NewServer(h)
	defer appServer.Close()
	cs, _ := a.store.cameras()
	var id string
	for _, c := range cs {
		if c.Status == "online" {
			id = c.ID
			break
		}
	}
	header := http.Header{}
	header.Set("Cookie", owner.String())
	conn, _, e := websocket.DefaultDialer.Dial(strings.Replace(appServer.URL, "http", "ws", 1)+"/api/cameras/"+id+"/ws", header)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, e := conn.ReadMessage()
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(data, []byte("fixture-secret")) || bytes.Contains(data, []byte("rtsp://")) {
		t.Fatal("private source leaked in media error")
	}
}
