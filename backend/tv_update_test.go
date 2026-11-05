package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTVUpdate(t *testing.T, dir string, apk []byte, meta string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, tvUpdateAPK), apk, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, tvUpdateMeta), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tvToken(t *testing.T, a *App, kind string) func(*http.Request) {
	t.Helper()
	tok := randomToken(32)
	var scope any
	if kind == "share" {
		scope = "[]"
	}
	if _, err := a.store.DB.Exec("INSERT INTO sessions(hash,name,kind,expires,cameras) VALUES(?,?,?,?,?)", tokenHash(tok), "TV", kind, time.Now().Add(time.Hour).Unix(), scope); err != nil {
		t.Fatal(err)
	}
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func TestTVUpdateServesNewestAPKWithComputedHash(t *testing.T) {
	a, h := testApp(t, "demo")
	setupOwner(t, a, h)
	a.cfg.tvUpdateDir = t.TempDir()
	apk := []byte("PK\x03\x04 fixture apk bytes")
	// A sha256 in the metadata is ignored; the hub hashes the file itself.
	writeTVUpdate(t, a.cfg.tvUpdateDir, apk, `{"versionCode":7,"versionName":"1.3.1","sha256":"00"}`)
	auth := tvToken(t, a, "tv")

	r := doTestWith(t, h, "GET", "/api/tv/update", nil, nil, auth)
	if r.Code != 200 {
		t.Fatalf("update info %d: %s", r.Code, r.Body.String())
	}
	var got tvUpdate
	if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(apk)
	if got.VersionCode != 7 || got.VersionName != "1.3.1" || got.SHA256 != hex.EncodeToString(sum[:]) || got.Size != int64(len(apk)) {
		t.Fatalf("update info %+v", got)
	}
	r = doTestWith(t, h, "GET", "/api/tv/update.apk", nil, nil, auth)
	if r.Code != 200 || r.Body.String() != string(apk) || r.Header().Get("Content-Type") != "application/vnd.android.package-archive" || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("apk %d %q %v", r.Code, r.Body.String(), r.Header())
	}
	// Resumable download after a dropped Wi-Fi connection.
	r = doTestWith(t, h, "GET", "/api/tv/update.apk", nil, nil, func(req *http.Request) { auth(req); req.Header.Set("Range", "bytes=2-") })
	if r.Code != 206 || r.Body.String() != string(apk[2:]) {
		t.Fatalf("range %d %q", r.Code, r.Body.String())
	}

	// A replaced file is hashed again rather than served with the old hash.
	newer := []byte("PK\x03\x04 a newer build")
	writeTVUpdate(t, a.cfg.tvUpdateDir, newer, `{"versionCode":8,"versionName":"1.3.2"}`)
	future := time.Now().Add(time.Minute)
	os.Chtimes(filepath.Join(a.cfg.tvUpdateDir, tvUpdateAPK), future, future)
	r = doTestWith(t, h, "GET", "/api/tv/update", nil, nil, auth)
	json.Unmarshal(r.Body.Bytes(), &got)
	sum = sha256.Sum256(newer)
	if got.VersionCode != 8 || got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("stale update info %+v", got)
	}
}

func TestTVUpdateNeedsTVOrOwnerAndARelease(t *testing.T) {
	a, h := testApp(t, "demo")
	cookie := setupOwner(t, a, h)
	a.cfg.tvUpdateDir = t.TempDir()

	if r := doTest(t, h, "GET", "/api/tv/update", nil, nil); r.Code != 401 {
		t.Fatalf("anonymous %d", r.Code)
	}
	if r := doTestWith(t, h, "GET", "/api/tv/update.apk", nil, nil, tvToken(t, a, "share")); r.Code != 403 {
		t.Fatalf("share link %d", r.Code)
	}
	// Nothing published yet: the TV treats 404 as "no update".
	if r := doTest(t, h, "GET", "/api/tv/update", nil, cookie); r.Code != 404 {
		t.Fatalf("empty folder %d", r.Code)
	}
	for _, meta := range []string{`{"versionCode":0,"versionName":"1.0"}`, `{"versionCode":7,"versionName":"1.3.1\"><x"}`, `not json`} {
		writeTVUpdate(t, a.cfg.tvUpdateDir, []byte("PK"), meta)
		if r := doTest(t, h, "GET", "/api/tv/update", nil, cookie); r.Code != 404 {
			t.Fatalf("bad metadata %s served: %d", meta, r.Code)
		}
	}
	writeTVUpdate(t, a.cfg.tvUpdateDir, []byte("PK"), `{"versionCode":7,"versionName":"1.3.1"}`)
	if r := doTest(t, h, "GET", "/api/tv/update", nil, cookie); r.Code != 200 {
		t.Fatalf("owner %d", r.Code)
	}
}

func TestTVUpdateNeverServesFromDataFolder(t *testing.T) {
	a, h := testApp(t, "demo")
	cookie := setupOwner(t, a, h)
	a.cfg.tvUpdateDir = filepath.Join(a.data, "tv")
	writeTVUpdate(t, a.cfg.tvUpdateDir, []byte("PK"), `{"versionCode":7,"versionName":"1.3.1"}`)
	if r := doTest(t, h, "GET", "/api/tv/update.apk", nil, cookie); r.Code != 404 {
		t.Fatalf("served from data folder: %d", r.Code)
	}
	a.cfg.tvUpdateDir = a.data
	if r := doTest(t, h, "GET", "/api/tv/update", nil, cookie); r.Code != 404 {
		t.Fatalf("served data folder itself: %d", r.Code)
	}
	if underDir(filepath.Join(a.data+"-sibling"), a.data) {
		t.Fatal("sibling folder treated as inside data")
	}
}
