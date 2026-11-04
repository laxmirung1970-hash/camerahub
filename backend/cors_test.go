package main

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func upgradeFixture(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return (&websocket.Upgrader{}).Upgrade(w, r, nil)
}

func TestParseOrigins(t *testing.T) {
	got := parseOrigins(" https://UI.Example.app ,bad, https://x.test/path, http://localhost:5173, https://u:p@h.test, *")
	want := map[string]bool{"https://ui.example.app": true, "http://localhost:5173": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed %v", got)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Protocol", "bearer, fixture-token")
	if bearerToken(req) != "fixture-token" {
		t.Fatal("subprotocol token not read")
	}
	req.Header.Del("Upgrade")
	if bearerToken(req) != "" {
		t.Fatal("subprotocol accepted on a plain request")
	}
}

func TestCrossOriginIsOffByDefault(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	origin := func(r *http.Request) { r.Header.Set("Origin", "https://ui.example.app") }
	r := doTestWith(t, h, "GET", "/api/bootstrap", nil, nil, origin)
	if r.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS header emitted without allowlist")
	}
	if r = doTestWith(t, h, "POST", "/api/logout", map[string]string{}, owner, origin); r.Code != 403 {
		t.Fatal("foreign origin mutation accepted")
	}
	if r = doTestWith(t, h, "OPTIONS", "/api/login", nil, nil, origin); r.Code == 204 {
		t.Fatal("preflight answered without allowlist")
	}
	login := doTest(t, h, "POST", "/api/login", map[string]string{"password": "fixture-owner-password-123"}, nil)
	if login.Code != 200 || len(login.Result().Cookies()) != 1 || strings.Contains(login.Body.String(), "token") {
		t.Fatal("same-origin login changed")
	}
	login = doTest(t, h, "POST", "/api/login", map[string]any{"password": "fixture-owner-password-123", "token": true}, nil)
	if login.Code != 200 || len(login.Result().Cookies()) != 1 || !strings.Contains(login.Body.String(), `"token"`) {
		t.Fatal("explicit token request ignored")
	}
}

func TestAllowlistedOriginUsesBearerWithoutCookies(t *testing.T) {
	t.Setenv("APP_ALLOWED_ORIGINS", "https://UI.example.app")
	a, h := testApp(t, "demo")
	cross := func(r *http.Request) {
		r.Header.Set("Origin", "https://ui.example.app")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	}
	r := doTestWith(t, h, "OPTIONS", "/api/login", nil, nil, func(r *http.Request) {
		cross(r)
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	})
	if r.Code != 204 || r.Header().Get("Access-Control-Allow-Origin") != "https://ui.example.app" || r.Header().Get("Access-Control-Allow-Credentials") != "true" || !strings.Contains(r.Header().Get("Access-Control-Allow-Headers"), "Authorization") || r.Header().Get("Vary") != "Origin" {
		t.Fatalf("preflight %d %v", r.Code, r.Header())
	}
	r = doTestWith(t, h, "POST", "/api/setup", map[string]string{"code": a.setupCode(), "name": "Owner", "password": "fixture-owner-password-123"}, nil, cross)
	if r.Code != 201 || len(r.Result().Cookies()) != 0 {
		t.Fatalf("cross-origin setup %d cookies=%d", r.Code, len(r.Result().Cookies()))
	}
	var body struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	if body.Token == "" {
		t.Fatal("no token for cross-origin client")
	}
	bearer := func(r *http.Request) {
		cross(r)
		r.Header.Set("Authorization", "Bearer "+body.Token)
	}
	if r = doTestWith(t, h, "GET", "/api/system", nil, nil, bearer); r.Code != 200 || r.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("owner bearer rejected: %d", r.Code)
	}
	if r = doTestWith(t, h, "PATCH", "/api/cameras/unknown", map[string]string{"name": "x"}, nil, bearer); r.Code != 404 {
		t.Fatalf("cross-origin mutation with bearer blocked: %d", r.Code)
	}
	if r = doTestWith(t, h, "POST", "/api/login", map[string]string{"password": "fixture-owner-password-123"}, nil, func(r *http.Request) { r.Header.Set("Origin", "https://other.example") }); r.Code != 403 {
		t.Fatal("unlisted origin accepted")
	}
	cs, _ := a.store.cameras()
	if r = doTestWith(t, h, "GET", "/api/cameras/"+cs[0].ID+"/hls", nil, nil, bearer); r.Header().Get("Access-Control-Allow-Origin") != "https://ui.example.app" {
		t.Fatal("HLS playlist missing CORS header")
	}
	gateway := readyGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ws" {
			conn, e := upgradeFixture(w, r)
			if e == nil {
				_ = conn.WriteJSON(wsMessage{Type: "mse", Value: "video/mp4"})
				_, _, _ = conn.ReadMessage()
				conn.Close()
			}
			return
		}
		w.WriteHeader(200)
	})
	a.bridge.Base = gateway.URL
	appServer := httptest.NewServer(h)
	defer appServer.Close()
	wsURL := strings.Replace(appServer.URL, "http", "ws", 1) + "/api/cameras/" + cs[0].ID + "/ws"
	dialer := websocket.Dialer{Subprotocols: []string{"bearer", body.Token}}
	header := http.Header{"Origin": {"https://ui.example.app"}}
	conn, res, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("subprotocol bearer rejected: %v", err)
	}
	defer conn.Close()
	if res.Header.Get("Sec-WebSocket-Protocol") != "bearer" {
		t.Fatal("server did not echo the bearer subprotocol")
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err = conn.ReadMessage(); err != nil {
		t.Fatal("no media negotiation relayed: ", err)
	}
	if _, res, err = websocket.DefaultDialer.Dial(wsURL+"?token="+body.Token, header); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatal("query-string token accepted")
	}
	if _, res, err = dialer.Dial(wsURL, http.Header{"Origin": {"https://other.example"}}); err == nil || res == nil || res.StatusCode != 403 {
		t.Fatal("unlisted origin reached the media socket")
	}
}
