package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/argon2"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func tokenHash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func hashPassword(p string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	out := argon2.IDKey([]byte(p), salt, 2, 32*1024, 2, 32)
	return base64.RawStdEncoding.EncodeToString(salt) + "." + base64.RawStdEncoding.EncodeToString(out)
}
func checkPassword(p, encoded string) bool {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[0])
	if e != nil || len(salt) != 16 {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(parts[1])
	if e != nil || len(expected) != 32 {
		return false
	}
	out := argon2.IDKey([]byte(p), salt, 2, 32*1024, 2, 32)
	return subtle.ConstantTimeCompare(out, expected) == 1
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	jsonOut(w, status, map[string]string{"error": msg})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "ข้อมูลไม่ถูกต้อง กรุณาตรวจสอบช่องที่กรอก")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "ส่งข้อมูลได้ครั้งละหนึ่งรายการ")
		return false
	}
	return true
}
func (a *App) setupCode() string { return a.store.setting("setup_code") }
func (a *App) initSetup(dir string) error {
	if a.store.setting("owner_hash") != "" {
		return nil
	}
	code := a.setupCode()
	if code == "" {
		code = randomToken(18)
		if err := a.store.setSetting("setup_code", code); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, a.mode+"-setup-code.txt"), []byte(code), 0600)
}
func (a *App) issueSession(w http.ResponseWriter, r *http.Request, name, kind string) (string, error) {
	tok := randomToken(32)
	days := 14
	if kind == "tv" {
		days = 90
	}
	_, err := a.store.DB.Exec("INSERT INTO sessions(hash,name,kind,expires) VALUES(?,?,?,?)", tokenHash(tok), name, kind, time.Now().Add(time.Duration(days)*24*time.Hour).Unix())
	if err != nil {
		return "", err
	}
	if kind != "tv" {
		http.SetCookie(w, &http.Cookie{Name: "ch_session", Value: tok, Path: "/", HttpOnly: true, Secure: r.TLS != nil || env("APP_SECURE_COOKIES", "false") == "true", SameSite: http.SameSiteStrictMode, MaxAge: days * 86400})
	}
	return tok, nil
}
func (a *App) principal(r *http.Request) (Principal, bool) {
	tok := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimPrefix(h, "Bearer ")
	} else if cookie, err := r.Cookie("ch_session"); err == nil {
		tok = cookie.Value
	}
	if tok == "" {
		return Principal{}, false
	}
	var p Principal
	var expires int64
	p.ID = tokenHash(tok)
	err := a.store.DB.QueryRow("SELECT name,kind,expires FROM sessions WHERE hash=?", p.ID).Scan(&p.Name, &p.Kind, &expires)
	return p, err == nil && expires > time.Now().Unix()
}
func (a *App) require(owner bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.principal(r)
		if !ok {
			fail(w, 401, "กรุณาเข้าสู่ระบบ")
			return
		}
		if owner && p.Kind != "owner" {
			fail(w, 403, "ต้องใช้บัญชีเจ้าของระบบ")
			return
		}
		next(w, r)
	}
}
func (a *App) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self' ws: wss:; font-src 'self'; worker-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		// Reject cross-site mutations, including form posts. Bearer clients are not ambient credentials.
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					fail(w, 403, "ไม่อนุญาตคำขอจากเว็บไซต์อื่น")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "ไม่อนุญาตคำขอจากเว็บไซต์อื่น")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (a *App) rateOK(r *http.Request, kind string, limit int) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := kind + ":" + ip
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	b := a.rates[key]
	if now.Sub(b.At) > time.Minute {
		b = rateBucket{At: now}
	}
	b.Count++
	a.rates[key] = b
	if len(a.rates) > 2000 {
		for k, v := range a.rates {
			if now.Sub(v.At) > 2*time.Minute {
				delete(a.rates, k)
			}
		}
	}
	return b.Count <= limit
}
func (a *App) authRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.principal(r)
		jsonOut(w, 200, map[string]any{"mode": a.mode, "needsSetup": a.store.setting("owner_hash") == "", "authenticated": ok, "name": p.Name, "kind": p.Kind, "version": "1.0.0", "webRTC": env("APP_WEBRTC", "false") == "true"})
	})
	m.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if !a.rateOK(r, "setup", 8) {
			fail(w, 429, "ลองใหม่ในอีกหนึ่งนาที")
			return
		}
		var b struct{ Code, Name, Password string }
		if !readJSON(w, r, &b) {
			return
		}
		if utf8.RuneCountInString(b.Password) < 8 || len(b.Password) > 256 || len(strings.TrimSpace(b.Name)) < 2 || len(b.Name) > 80 {
			fail(w, 400, "ระบุชื่อและรหัสผ่านอย่างน้อย 8 ตัวอักษร")
			return
		}
		a.setupMu.Lock()
		defer a.setupMu.Unlock()
		if a.store.setting("owner_hash") != "" {
			fail(w, 409, "ตั้งค่าเจ้าของระบบแล้ว")
			return
		}
		if a.setupCode() == "" || subtle.ConstantTimeCompare([]byte(b.Code), []byte(a.setupCode())) != 1 {
			fail(w, 403, "รหัสเริ่มต้นไม่ถูกต้อง ดูไฟล์รหัสตั้งค่าในโฟลเดอร์ข้อมูล")
			return
		}
		tx, e := a.store.DB.Begin()
		if e != nil {
			fail(w, 500, "บันทึกไม่สำเร็จ")
			return
		}
		defer tx.Rollback()
		for k, v := range map[string]string{"owner_hash": hashPassword(b.Password), "owner_name": strings.TrimSpace(b.Name), "setup_code": ""} {
			if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v); e != nil {
				fail(w, 500, "บันทึกไม่สำเร็จ")
				return
			}
		}
		if tx.Commit() != nil {
			fail(w, 500, "บันทึกไม่สำเร็จ")
			return
		}
		_ = os.Remove(filepath.Join(a.data, a.mode+"-setup-code.txt"))
		if _, e = a.issueSession(w, r, b.Name, "owner"); e != nil {
			fail(w, 500, "สร้างเซสชันไม่สำเร็จ")
			return
		}
		a.store.event("setup", "ตั้งค่าเจ้าของระบบเรียบร้อย")
		jsonOut(w, 201, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		if !a.rateOK(r, "login", 8) {
			fail(w, 429, "ลองเข้าสู่ระบบใหม่ในอีกหนึ่งนาที")
			return
		}
		var b struct{ Password string }
		if !readJSON(w, r, &b) {
			return
		}
		if len(b.Password) > 256 || !checkPassword(b.Password, a.store.setting("owner_hash")) {
			fail(w, 401, "รหัสผ่านไม่ถูกต้อง")
			return
		}
		if _, err := a.issueSession(w, r, a.store.setting("owner_name"), "owner"); err != nil {
			fail(w, 500, "เข้าใช้งานไม่สำเร็จ")
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/logout", a.require(false, func(w http.ResponseWriter, r *http.Request) {
		p, _ := a.principal(r)
		_, _ = a.store.DB.Exec("DELETE FROM sessions WHERE hash=?", p.ID)
		a.stopPrincipal(p.ID)
		http.SetCookie(w, &http.Cookie{Name: "ch_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))
	m.HandleFunc("GET /api/devices", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		rows, err := a.store.DB.Query("SELECT hash,name,expires FROM sessions WHERE kind='tv' AND expires>?", time.Now().Unix())
		if err != nil {
			fail(w, 500, "อ่านอุปกรณ์ไม่สำเร็จ")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var hash, name string
			var expires int64
			if rows.Scan(&hash, &name, &expires) == nil {
				out = append(out, map[string]any{"id": hash, "name": name, "expires": expires})
			}
		}
		jsonOut(w, 200, out)
	}))
	m.HandleFunc("DELETE /api/devices/{id}", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		_, _ = a.store.DB.Exec("DELETE FROM sessions WHERE hash=? AND kind='tv'", id)
		a.stopPrincipal(id)
		jsonOut(w, 200, map[string]bool{"ok": true})
	}))
}
func safeError(prefix string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: การเชื่อมต่อไม่สำเร็จ โปรดตรวจบัญชี ที่อยู่กล้อง และสถานะเครือข่าย", prefix)
}
