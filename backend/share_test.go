package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func createShare(t *testing.T, h http.Handler, owner *http.Cookie, cameras []string) string {
	t.Helper()
	r := doTest(t, h, "POST", "/api/shares", map[string]any{"name": "ห้องอาม่า", "cameras": cameras}, owner)
	if r.Code != 201 {
		t.Fatalf("share %d: %s", r.Code, r.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &body)
	if body.Token == "" {
		t.Fatal("share token missing")
	}
	return body.Token
}

func TestShareLinkSeesOnlyItsCameras(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	cs, _ := a.store.cameras()
	if len(cs) < 3 {
		t.Fatal("demo registry too small for the scope test")
	}
	if r := doTest(t, h, "POST", "/api/shares", map[string]any{"name": "x", "cameras": []string{}}, owner); r.Code != 400 {
		t.Fatalf("empty camera list accepted: %d", r.Code)
	}
	if r := doTest(t, h, "POST", "/api/shares", map[string]any{"name": "x", "cameras": []string{"xiaomi-unknown"}}, owner); r.Code != 404 {
		t.Fatalf("unknown camera accepted: %d", r.Code)
	}
	if r := doTest(t, h, "POST", "/api/shares", map[string]any{"name": "x", "cameras": []string{cs[0].ID}}, nil); r.Code != 401 {
		t.Fatalf("anonymous share creation: %d", r.Code)
	}
	token := createShare(t, h, owner, []string{cs[0].ID, cs[1].ID, cs[0].ID})
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }

	r := doTestWith(t, h, "GET", "/api/bootstrap", nil, nil, bearer)
	var boot struct {
		Authenticated bool   `json:"authenticated"`
		Kind          string `json:"kind"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &boot)
	if !boot.Authenticated || boot.Kind != "share" {
		t.Fatalf("bootstrap for share token: %s", r.Body.String())
	}
	r = doTestWith(t, h, "GET", "/api/cameras", nil, nil, bearer)
	var listed []Camera
	_ = json.Unmarshal(r.Body.Bytes(), &listed)
	if r.Code != 200 || len(listed) != 2 {
		t.Fatalf("share list %d: %s", r.Code, r.Body.String())
	}
	for _, c := range listed {
		if c.ID != cs[0].ID && c.ID != cs[1].ID {
			t.Fatalf("camera outside scope listed: %s", c.ID)
		}
		if c.Host != "" || c.Ref != "" {
			t.Fatalf("share list leaks host/ref: %+v", c)
		}
	}
	if r = doTestWith(t, h, "GET", "/api/cameras/"+cs[2].ID+"/hls", nil, nil, bearer); r.Code != 404 {
		t.Fatalf("out-of-scope hls %d", r.Code)
	}
	if r = doTestWith(t, h, "GET", "/api/cameras/"+cs[2].ID+"/ws", nil, nil, bearer); r.Code != 404 {
		t.Fatalf("out-of-scope ws %d", r.Code)
	}
	if r = doTestWith(t, h, "POST", "/api/cameras/"+cs[0].ID+"/ptz", map[string]string{"direction": "left"}, nil, bearer); r.Code != 403 {
		t.Fatalf("share ptz %d", r.Code)
	}
	if r = doTestWith(t, h, "GET", "/api/system", nil, nil, bearer); r.Code != 403 {
		t.Fatalf("share reached owner route %d", r.Code)
	}
	if r = doTestWith(t, h, "POST", "/api/shares", map[string]any{"name": "y", "cameras": []string{cs[0].ID}}, nil, bearer); r.Code != 403 {
		t.Fatalf("share minted another share %d", r.Code)
	}
	// Owner and TV sessions are unchanged: the owner still sees the whole registry.
	r = doTest(t, h, "GET", "/api/cameras", nil, owner)
	var all []Camera
	_ = json.Unmarshal(r.Body.Bytes(), &all)
	if len(all) != len(cs) {
		t.Fatalf("owner list changed: %d cameras", len(all))
	}
}

func TestShareLinkListedAndRevokedWithDevices(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	cs, _ := a.store.cameras()
	token := createShare(t, h, owner, []string{cs[0].ID, cs[1].ID})
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
	r := doTest(t, h, "GET", "/api/devices", nil, owner)
	var devices []struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Kind    string   `json:"kind"`
		Cameras []string `json:"cameras"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &devices)
	if len(devices) != 1 || devices[0].Kind != "share" || devices[0].ID != tokenHash(token) || len(devices[0].Cameras) != 2 || devices[0].Name != "ห้องอาม่า" {
		t.Fatalf("devices %s", r.Body.String())
	}
	if r = doTest(t, h, "DELETE", "/api/devices/"+devices[0].ID, nil, owner); r.Code != 200 {
		t.Fatalf("revoke %d", r.Code)
	}
	if r = doTestWith(t, h, "GET", "/api/cameras", nil, nil, bearer); r.Code != 401 {
		t.Fatalf("revoked share still works: %d", r.Code)
	}
	if r = doTest(t, h, "GET", "/api/devices", nil, owner); r.Body.String() != "[]\n" {
		t.Fatalf("devices after revoke: %s", r.Body.String())
	}
}

func TestSessionsCameraColumnMigrationIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		s, err := openStore(dir, "demo")
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if _, err = s.DB.Exec("INSERT INTO sessions(hash,name,kind,expires,cameras) VALUES(?,?,?,?,NULL)", "h"+string(rune('a'+i)), "n", "tv", 1); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		s.DB.Close()
	}
}
