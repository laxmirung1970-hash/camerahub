package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	hash, err := a.store.lookupSetting("owner_hash")
	if err != nil || hash != "" {
		return err
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

// issueSession stores a hashed token. The cookie is skipped for TV and share
// tokens and for allowlisted cross-origin logins, which keep the token themselves.
// cameras is nil for owner/TV; a share link stores its exact camera allowlist.
func (a *App) issueSession(w http.ResponseWriter, r *http.Request, name, kind string, cookie bool, cameras []string) (string, error) {
	tok := randomToken(32)
	days := 14
	switch kind {
	case "tv":
		days = 90
	case "share":
		days = 365
	}
	var scope any
	if cameras != nil {
		raw, err := json.Marshal(cameras)
		if err != nil {
			return "", err
		}
		scope = string(raw)
	}
	_, err := a.store.DB.Exec("INSERT INTO sessions(hash,name,kind,expires,cameras) VALUES(?,?,?,?,?)", tokenHash(tok), name, kind, time.Now().Add(time.Duration(days)*24*time.Hour).Unix(), scope)
	if err != nil {
		return "", err
	}
	if cookie {
		http.SetCookie(w, &http.Cookie{Name: "ch_session", Value: tok, Path: "/", HttpOnly: true, Secure: r.TLS != nil || a.cfg.secureCookies, SameSite: http.SameSiteStrictMode, MaxAge: days * 86400})
	}
	return tok, nil
}
func (a *App) finishLogin(w http.ResponseWriter, r *http.Request, name string, wantToken bool, status int) {
	cross := a.crossOriginAllowed(r)
	tok, err := a.issueSession(w, r, name, "owner", !cross, nil)
	if err != nil {
		fail(w, 500, "สร้างเซสชันไม่สำเร็จ")
		return
	}
	out := map[string]any{"ok": true}
	if cross || wantToken {
		out["token"] = tok
	}
	jsonOut(w, status, out)
}

// Browsers cannot set headers on a WebSocket handshake, so a cross-origin
// client sends its token as the subprotocol after "bearer". Query strings
// are never accepted because they end up in logs.
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return ""
	}
	protocols := strings.Split(strings.Join(r.Header.Values("Sec-WebSocket-Protocol"), ","), ",")
	for i, p := range protocols {
		if strings.TrimSpace(p) == "bearer" && i+1 < len(protocols) {
			return strings.TrimSpace(protocols[i+1])
		}
	}
	return ""
}
func (a *App) principal(r *http.Request) (Principal, bool) {
	tok := bearerToken(r)
	if tok == "" {
		if cookie, err := r.Cookie("ch_session"); err == nil {
			tok = cookie.Value
		}
	}
	if tok == "" {
		return Principal{}, false
	}
	var p Principal
	var expires int64
	var scope sql.NullString
	p.ID = tokenHash(tok)
	err := a.store.DB.QueryRow("SELECT name,kind,expires,cameras FROM sessions WHERE hash=?", p.ID).Scan(&p.Name, &p.Kind, &expires, &scope)
	if err != nil || expires <= time.Now().Unix() {
		return Principal{}, false
	}
	if scope.Valid {
		if json.Unmarshal([]byte(scope.String), &p.Cameras) != nil || p.Cameras == nil {
			// A share row whose scope cannot be read must not widen to every camera.
			p.Cameras = []string{}
		}
	}
	return p, true
}

type principalKey struct{}

func principalOf(r *http.Request) Principal {
	p, _ := r.Context().Value(principalKey{}).(Principal)
	return p
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
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	}
}
func (a *App) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self' ws: wss:; font-src 'self'; worker-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		origin := r.Header.Get("Origin")
		allowed := origin != "" && a.crossOriginAllowed(r)
		if allowed {
			// Exact origin echo; never "*". Credentials stay off unless the
			// origin is on the allowlist.
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Expose-Headers", "Content-Disposition")
			h.Add("Vary", "Origin")
			if r.Method == "OPTIONS" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(204)
				return
			}
		}
		// Reject cross-site mutations, including form posts. Bearer clients are not ambient credentials.
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && !allowed {
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

// clientIP trusts X-Forwarded-For only from the one configured proxy, and
// only its rightmost entry, which that proxy wrote itself.
func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a.cfg.trustedProxy == "" || host != a.cfg.trustedProxy {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
		return ip
	}
	return host
}
func (a *App) rateOK(r *http.Request, kind string, limit int) bool {
	key := kind + ":" + a.clientIP(r)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if now.Sub(a.ratesPruned) > time.Minute {
		for k, v := range a.rates {
			if now.Sub(v.At) > 2*time.Minute {
				delete(a.rates, k)
			}
		}
		a.ratesPruned = now
	}
	b := a.rates[key]
	if now.Sub(b.At) > time.Minute {
		b = rateBucket{At: now}
	}
	b.Count++
	a.rates[key] = b
	return b.Count <= limit
}
func (a *App) authRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.principal(r)
		jsonOut(w, 200, map[string]any{"mode": a.mode, "needsSetup": a.store.setting("owner_hash") == "", "authenticated": ok, "name": p.Name, "kind": p.Kind, "version": appVersion, "webRTC": a.cfg.webRTC})
	})
	m.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if !a.limit(w, r, "setup", 8, "ลองใหม่ในอีกหนึ่งนาที") {
			return
		}
		var b struct {
			Code, Name, Password string
			Token                bool
		}
		if !readJSON(w, r, &b) {
			return
		}
		name := strings.TrimSpace(b.Name)
		if utf8.RuneCountInString(b.Password) < 8 || len(b.Password) > 256 || runes(name) < 2 || runes(name) > 80 {
			fail(w, 400, "ระบุชื่อและรหัสผ่านอย่างน้อย 8 ตัวอักษร")
			return
		}
		a.setupMu.Lock()
		defer a.setupMu.Unlock()
		hash, err := a.store.lookupSetting("owner_hash")
		if err != nil {
			fail(w, 500, "อ่านการตั้งค่าไม่สำเร็จ")
			return
		}
		if hash != "" {
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
		for k, v := range map[string]string{"owner_hash": hashPassword(b.Password), "owner_name": name, "setup_code": ""} {
			if upsertSetting(tx, k, v) != nil {
				fail(w, 500, "บันทึกไม่สำเร็จ")
				return
			}
		}
		if tx.Commit() != nil {
			fail(w, 500, "บันทึกไม่สำเร็จ")
			return
		}
		_ = os.Remove(filepath.Join(a.data, a.mode+"-setup-code.txt"))
		a.store.event("setup", "ตั้งค่าเจ้าของระบบเรียบร้อย")
		a.finishLogin(w, r, name, b.Token, 201)
	})
	m.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		if !a.limit(w, r, "login", 8, "ลองเข้าสู่ระบบใหม่ในอีกหนึ่งนาที") {
			return
		}
		var b struct {
			Password string
			Token    bool
		}
		if !readJSON(w, r, &b) {
			return
		}
		if len(b.Password) > 256 || !checkPassword(b.Password, a.store.setting("owner_hash")) {
			fail(w, 401, "รหัสผ่านไม่ถูกต้อง")
			return
		}
		a.finishLogin(w, r, a.store.setting("owner_name"), b.Token, 200)
	})
	m.HandleFunc("POST /api/logout", a.require(false, func(w http.ResponseWriter, r *http.Request) {
		p := principalOf(r)
		if _, err := a.store.DB.Exec("DELETE FROM sessions WHERE hash=?", p.ID); err != nil {
			fail(w, 500, "ออกจากระบบไม่สำเร็จ")
			return
		}
		a.stopPrincipal(p.ID)
		http.SetCookie(w, &http.Cookie{Name: "ch_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		ok(w)
	}))
	m.HandleFunc("GET /api/devices", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		rows, err := a.store.DB.Query("SELECT hash,name,kind,expires,cameras FROM sessions WHERE kind IN ('tv','share') AND expires>?", time.Now().Unix())
		if err != nil {
			fail(w, 500, "อ่านอุปกรณ์ไม่สำเร็จ")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var hash, name, kind string
			var expires int64
			var scope sql.NullString
			if rows.Scan(&hash, &name, &kind, &expires, &scope) == nil {
				item := map[string]any{"id": hash, "name": name, "kind": kind, "expires": expires}
				if scope.Valid {
					cameras := []string{}
					_ = json.Unmarshal([]byte(scope.String), &cameras)
					item["cameras"] = cameras
				}
				out = append(out, item)
			}
		}
		jsonOut(w, 200, out)
	}))
	m.HandleFunc("DELETE /api/devices/{id}", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, err := a.store.DB.Exec("DELETE FROM sessions WHERE hash=? AND kind IN ('tv','share')", id); err != nil {
			fail(w, 500, "ยกเลิกอุปกรณ์ไม่สำเร็จ")
			return
		}
		a.stopPrincipal(id)
		ok(w)
	}))
	// A share link is a view-only session scoped to a few cameras. The token is
	// returned once; the link that carries it is the credential.
	m.HandleFunc("POST /api/shares", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Name    string   `json:"name"`
			Cameras []string `json:"cameras"`
		}
		if !readJSON(w, r, &b) {
			return
		}
		b.Name = strings.TrimSpace(b.Name)
		if b.Name == "" || runes(b.Name) > 80 {
			fail(w, 400, "ระบุชื่อลิงก์ไม่เกิน 80 ตัวอักษร")
			return
		}
		if len(b.Cameras) == 0 || len(b.Cameras) > 8 {
			fail(w, 400, "เลือกกล้อง 1 ถึง 8 ตัว")
			return
		}
		seen := map[string]bool{}
		ids := make([]string, 0, len(b.Cameras))
		for _, id := range b.Cameras {
			if seen[id] {
				continue
			}
			if _, err := a.store.camera(id); err != nil {
				fail(w, 404, "ไม่พบกล้อง")
				return
			}
			seen[id] = true
			ids = append(ids, id)
		}
		tok, err := a.issueSession(w, r, b.Name, "share", false, ids)
		if err != nil {
			fail(w, 500, "สร้างลิงก์แชร์ไม่สำเร็จ")
			return
		}
		a.store.event("share", "สร้างลิงก์แชร์ "+b.Name)
		jsonOut(w, 201, map[string]string{"token": tok})
	}))
}
