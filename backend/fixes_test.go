package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func doTestWith(t *testing.T, h http.Handler, method, path string, body any, cookie *http.Cookie, mutate func(*http.Request)) *httptest.ResponseRecorder {
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
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// A gateway stub that accepts every registration and answers health checks.
func readyGateway(t *testing.T, extra http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" {
			_, _ = io.WriteString(w, `{"startup":"ready"}`)
			return
		}
		if extra != nil {
			extra(w, r)
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestProbeDoesNotOverwriteConcurrentRename(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	connected := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	gateway := readyGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ws" {
			w.WriteHeader(200)
			return
		}
		conn, e := upgradeFixture(w, r)
		if e != nil {
			return
		}
		defer conn.Close()
		once.Do(func() { close(connected) })
		<-release
		_ = conn.WriteJSON(wsMessage{Type: "error", Value: "fixture"})
	})
	a.bridge.Base = gateway.URL
	cs, _ := a.store.cameras()
	id := cs[0].ID
	probed := make(chan *httptest.ResponseRecorder, 1)
	go func() { probed <- doTest(t, h, "POST", "/api/cameras/"+id+"/probe", map[string]string{}, owner) }()
	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("probe never reached the gateway")
	}
	if r := doTest(t, h, "PATCH", "/api/cameras/"+id, map[string]string{"name": "ชื่อใหม่ระหว่างตรวจ"}, owner); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	close(release)
	r := <-probed
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	saved, _ := a.store.camera(id)
	if saved.Name != "ชื่อใหม่ระหว่างตรวจ" || saved.Status != "error" {
		t.Fatalf("probe result overwrote rename: %q %s", saved.Name, saved.Status)
	}
}

func TestBridgeRegistrationIsNotSerialisedAcrossStreams(t *testing.T) {
	release := make(chan struct{})
	var puts sync.Map
	gateway := readyGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			name := r.URL.Query().Get("name")
			n, _ := puts.LoadOrStore(name, new(int))
			*(n.(*int))++
			if name == "slow" {
				<-release
			}
		}
		w.WriteHeader(200)
	})
	b := &Bridge{Base: gateway.URL, Client: gateway.Client()}
	go func() { _ = b.register(context.Background(), "slow", "src") }()
	time.Sleep(50 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- b.register(context.Background(), "fast", "src") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("independent stream waited behind a slow registration")
	}
	close(release)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.register(context.Background(), "shared", "same-src")
		}()
	}
	wg.Wait()
	if n, _ := puts.Load("shared"); *(n.(*int)) != 1 {
		t.Fatalf("same stream registered %d times", *(n.(*int)))
	}
}

func TestClientIPTrustsOnlyConfiguredProxy(t *testing.T) {
	t.Setenv("APP_TRUSTED_PROXY", "10.0.0.1")
	a, _ := testApp(t, "demo")
	for _, tc := range []struct{ remote, forwarded, want string }{
		{"10.0.0.1:443", "198.51.100.7, 203.0.113.9", "203.0.113.9"},
		{"10.0.0.1:443", "", "10.0.0.1"},
		{"10.0.0.1:443", "not-an-ip", "10.0.0.1"},
		{"192.0.2.5:443", "203.0.113.9", "192.0.2.5"},
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.remote
		if tc.forwarded != "" {
			req.Header.Set("X-Forwarded-For", tc.forwarded)
		}
		if got := a.clientIP(req); got != tc.want {
			t.Fatalf("%s + %q = %s, want %s", tc.remote, tc.forwarded, got, tc.want)
		}
	}
	a.rates["stale:1.1.1.1"] = rateBucket{At: time.Now().Add(-10 * time.Minute)}
	a.ratesPruned = time.Now().Add(-2 * time.Minute)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.5:1"
	a.rateOK(req, "login", 8)
	if _, stale := a.rates["stale:1.1.1.1"]; stale {
		t.Fatal("old buckets were not pruned")
	}
}

func TestSessionDeleteFailuresAreReported(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	if _, err := a.store.DB.Exec("CREATE TRIGGER no_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'fixture'); END;"); err != nil {
		t.Fatal(err)
	}
	if r := doTest(t, h, "POST", "/api/logout", map[string]string{}, owner); r.Code != 500 {
		t.Fatalf("logout reported success on failed delete: %d", r.Code)
	}
	_, _ = a.store.DB.Exec("INSERT INTO sessions(hash,name,kind,expires) VALUES('tv-fixture','TV','tv',?)", time.Now().Add(time.Hour).Unix())
	if r := doTest(t, h, "DELETE", "/api/devices/tv-fixture", nil, owner); r.Code != 500 {
		t.Fatalf("device revoke reported success on failed delete: %d", r.Code)
	}
}

func TestWorkersStopAndScheduledSyncIsBounded(t *testing.T) {
	a, _ := testApp(t, "live")
	_ = a.store.saveConnection(Connection{ID: "c", Vendor: "xiaomi", Account: "123456", Region: "sg", Enabled: true})
	gateway := readyGateway(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	a.bridge.Base = gateway.URL
	a.bridge.Client = gateway.Client()
	start := time.Now()
	a.scheduledSync(context.Background(), "xiaomi", 200*time.Millisecond)
	if time.Since(start) > 3*time.Second {
		t.Fatal("scheduled sync was not bounded by its timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.background(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background worker ignored cancellation")
	}
}

func TestRegisterAllStreamsWaitsForGateway(t *testing.T) {
	a, _ := testApp(t, "demo")
	var mu sync.Mutex
	names := map[string]bool{}
	gateway := readyGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			mu.Lock()
			names[r.URL.Query().Get("name")] = true
			mu.Unlock()
		}
		w.WriteHeader(200)
	})
	a.bridge.Base = gateway.URL
	a.registerAllStreams(context.Background())
	cs, _ := a.store.cameras()
	mu.Lock()
	defer mu.Unlock()
	for _, c := range cs {
		if !names[streamName(c, "low")] {
			t.Fatalf("%s not registered", c.ID)
		}
	}
}

func TestCleanupRemovesExpiredSessionsAndPairs(t *testing.T) {
	a, _ := testApp(t, "demo")
	_, _ = a.store.DB.Exec("INSERT INTO sessions(hash,name,kind,expires) VALUES('old','x','tv',1),('new','y','tv',?)", time.Now().Add(time.Hour).Unix())
	a.pairs["old"] = &pairRequest{Expires: time.Now().Add(-time.Minute)}
	a.pairs["new"] = &pairRequest{Expires: time.Now().Add(time.Minute)}
	a.cleanupExpired()
	var n int
	_ = a.store.DB.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 1 || a.pairs["old"] != nil || a.pairs["new"] == nil {
		t.Fatal("cleanup kept expired or removed live entries")
	}
}

func TestTapoConnectionsAreKeyedByUsername(t *testing.T) {
	if connectionID("tapo", "", "First@example.test") == connectionID("tapo", "", "second@example.test") {
		t.Fatal("two Tapo accounts collapsed into one record")
	}
	if connectionID("tapo", "", " first@EXAMPLE.test ") != connectionID("tapo", "", "First@example.test") {
		t.Fatal("same account produced different records")
	}
	if connectionID("xiaomi", "123456", "ignored") != cameraID("connection", "xiaomi:123456") {
		t.Fatal("Xiaomi key changed")
	}
}

func TestLengthLimitsCountCharactersNotBytes(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	cs, _ := a.store.cameras()
	thai := strings.Repeat("ก", 180)
	if r := doTest(t, h, "PATCH", "/api/cameras/"+cs[0].ID, map[string]string{"name": thai}, owner); r.Code != 200 {
		t.Fatalf("180 Thai characters rejected: %s", r.Body.String())
	}
	if r := doTest(t, h, "PATCH", "/api/cameras/"+cs[0].ID, map[string]string{"name": thai + "ข"}, owner); r.Code != 400 {
		t.Fatal("181 characters accepted")
	}
	if r := doTest(t, h, "POST", "/api/pair/request", map[string]string{"name": strings.Repeat("ท", 80)}, nil); r.Code != 201 {
		t.Fatal("80-character Thai TV name rejected")
	}
	if r := doTest(t, h, "PATCH", "/api/cameras/unknown", map[string]string{"name": "x"}, owner); r.Code != 404 {
		t.Fatal("missing camera not reported")
	}
}

func TestBrokenSettingsTableIsAnError(t *testing.T) {
	a, h := testApp(t, "demo")
	if _, err := a.store.DB.Exec("DROP TABLE settings"); err != nil {
		t.Fatal(err)
	}
	if r := doTest(t, h, "POST", "/api/setup", map[string]string{"code": "x", "name": "Owner", "password": "fixture-owner-password"}, nil); r.Code != 500 {
		t.Fatalf("missing settings treated as unset: %d", r.Code)
	}
}

func TestStorePragmasApply(t *testing.T) {
	s, err := openStore(t.TempDir(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	var journal string
	var busy, foreign int
	_ = s.DB.QueryRow("PRAGMA journal_mode").Scan(&journal)
	_ = s.DB.QueryRow("PRAGMA busy_timeout").Scan(&busy)
	_ = s.DB.QueryRow("PRAGMA foreign_keys").Scan(&foreign)
	if journal != "wal" || busy != 5000 || foreign != 1 {
		t.Fatalf("pragmas not applied: %s %d %d", journal, busy, foreign)
	}
	if _, err = s.lookupSetting("missing"); err != nil {
		t.Fatal("unset key reported as error")
	}
}

func TestPlaceNewLenses(t *testing.T) {
	for _, tc := range []struct {
		order []string
		lens  map[string]string
		want  []string
	}{
		{[]string{"a", "b"}, nil, []string{"a", "b"}},
		{[]string{"a", "b", "a2"}, map[string]string{"a": "a2"}, []string{"a", "a2", "b"}},
		{[]string{"b", "a"}, map[string]string{"a": "a2", "b": "b2"}, []string{"b", "b2", "a", "a2"}},
	} {
		if got := placeNewLenses(tc.order, tc.lens); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%v + %v = %v", tc.order, tc.lens, got)
		}
	}
}
