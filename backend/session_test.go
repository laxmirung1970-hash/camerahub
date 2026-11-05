package main

import (
	"net/http"
	"testing"
	"time"
)

func sessionExpiry(t *testing.T, a *App, hash string) time.Time {
	t.Helper()
	var exp int64
	if err := a.store.DB.QueryRow("SELECT expires FROM sessions WHERE hash=?", hash).Scan(&exp); err != nil {
		t.Fatal(err)
	}
	return time.Unix(exp, 0)
}

func TestOwnerSessionSlidesAndRefreshesCookie(t *testing.T) {
	a, h := testApp(t, "demo")
	cookie := setupOwner(t, a, h)
	hash := tokenHash(cookie.Value)
	// Fresh sessions are not rewritten on every request.
	r := doTest(t, h, "GET", "/api/cameras", nil, cookie)
	if r.Code != 200 || len(r.Result().Cookies()) != 0 {
		t.Fatalf("fresh session: %d cookies=%d", r.Code, len(r.Result().Cookies()))
	}
	old := time.Now().Add(10 * 24 * time.Hour)
	if _, err := a.store.DB.Exec("UPDATE sessions SET expires=? WHERE hash=?", old.Unix(), hash); err != nil {
		t.Fatal(err)
	}
	r = doTest(t, h, "GET", "/api/cameras", nil, cookie)
	if r.Code != 200 {
		t.Fatalf("renew request %d", r.Code)
	}
	if got := sessionExpiry(t, a, hash); time.Until(got) < 179*24*time.Hour {
		t.Fatalf("owner expiry not extended: %v", got)
	}
	cs := r.Result().Cookies()
	if len(cs) != 1 || cs[0].Value != cookie.Value || cs[0].MaxAge != 180*86400 || !cs[0].HttpOnly {
		t.Fatalf("cookie not refreshed: %+v", cs)
	}
}

func TestTVSessionSlidesWithoutCookie(t *testing.T) {
	a, h := testApp(t, "demo")
	setupOwner(t, a, h)
	tok := randomToken(32)
	if _, err := a.store.DB.Exec("INSERT INTO sessions(hash,name,kind,expires) VALUES(?,?,?,?)", tokenHash(tok), "TV", "tv", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	r := doTestWith(t, h, "GET", "/api/cameras", nil, nil, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) })
	if r.Code != 200 || len(r.Result().Cookies()) != 0 {
		t.Fatalf("tv request %d cookies=%d", r.Code, len(r.Result().Cookies()))
	}
	if got := sessionExpiry(t, a, tokenHash(tok)); time.Until(got) < 364*24*time.Hour {
		t.Fatalf("tv expiry not extended: %v", got)
	}
}

func TestShareAndExpiredSessionsDoNotSlide(t *testing.T) {
	a, h := testApp(t, "demo")
	setupOwner(t, a, h)
	share, expired := randomToken(32), randomToken(32)
	soon := time.Now().Add(time.Hour).Unix()
	if _, err := a.store.DB.Exec("INSERT INTO sessions(hash,name,kind,expires,cameras) VALUES(?,?,?,?,?),(?,?,?,?,NULL)",
		tokenHash(share), "family", "share", soon, "[]", tokenHash(expired), "TV", "tv", time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	doTestWith(t, h, "GET", "/api/cameras", nil, nil, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+share) })
	if got := sessionExpiry(t, a, tokenHash(share)).Unix(); got != soon {
		t.Fatalf("share link expiry changed: %d != %d", got, soon)
	}
	r := doTestWith(t, h, "GET", "/api/cameras", nil, nil, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+expired) })
	if r.Code != 401 || time.Until(sessionExpiry(t, a, tokenHash(expired))) > 0 {
		t.Fatalf("expired session revived: %d", r.Code)
	}
}
