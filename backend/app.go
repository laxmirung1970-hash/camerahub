package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type rateBucket struct {
	At    time.Time
	Count int
}
type pairRequest struct {
	ID, Code, Secret, Name, Token string
	Expires                       time.Time
	Approved                      bool
}
type ptzLease struct {
	Owner  string
	Timer  *time.Timer
	Camera Camera
}
type App struct {
	store                         *Store
	cfg                           config
	mode, data                    string
	bridge                        *Bridge
	started                       time.Time
	mu, setupMu, syncMu, xiaomiMu sync.Mutex
	rates                         map[string]rateBucket
	ratesPruned                   time.Time
	pairs                         map[string]*pairRequest
	leases                        map[string]*ptzLease
	authPhase                     string
	authDeadline                  time.Time
}

func newApp(s *Store, data, bridgeURL string) (*App, error) {
	a := &App{store: s, cfg: loadConfig(), mode: s.Mode, data: data, bridge: &Bridge{Base: strings.TrimRight(bridgeURL, "/"), Client: &http.Client{Timeout: 20 * time.Second}}, started: time.Now(), rates: map[string]rateBucket{}, pairs: map[string]*pairRequest{}, leases: map[string]*ptzLease{}}
	if err := a.initSetup(data); err != nil {
		return nil, err
	}
	if s.Mode == "demo" {
		if err := a.seedDemo(); err != nil {
			return nil, err
		}
	} else if err := s.migrateXiaomiDualLenses(); err != nil {
		return nil, errors.New("cannot prepare Xiaomi lens channels")
	}
	if err := a.writeRelayHosts(); err != nil {
		return nil, errors.New("cannot write camera relay allowlist")
	}
	return a, nil
}
func cameraID(vendor, ref string) string {
	h := sha256.Sum256([]byte(vendor + ":" + ref))
	return vendor + "-" + hex.EncodeToString(h[:8])
}
func streamName(c Camera, quality string) string {
	if quality != "high" {
		quality = "low"
	}
	return c.ID + "_" + quality
}
func runes(s string) int       { return utf8.RuneCountInString(s) }
func ok(w http.ResponseWriter) { jsonOut(w, 200, map[string]bool{"ok": true}) }

// Shared handler guards. Each writes the response itself and returns false.
// cameraFromPath resolves {id} for the caller; a camera outside a share link's
// scope looks exactly like a missing one.
func (a *App) cameraFromPath(w http.ResponseWriter, r *http.Request) (Camera, bool) {
	id := r.PathValue("id")
	c, err := a.store.camera(id)
	if err != nil || !principalOf(r).allows(id) {
		fail(w, 404, "ไม่พบกล้อง")
		return Camera{}, false
	}
	return c, true
}
func cameraUnavailable(c Camera) bool { return c.Status == "offline" || c.Status == "auth_expired" }
func (a *App) liveOnly(msg string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.mode != "live" {
			fail(w, 409, msg)
			return
		}
		next(w, r)
	}
}
func (a *App) demoOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.mode != "demo" {
			fail(w, 404, "ใช้ได้เฉพาะโหมดสาธิต")
			return
		}
		next(w, r)
	}
}
func (a *App) limit(w http.ResponseWriter, r *http.Request, kind string, n int, msg string) bool {
	if a.rateOK(r, kind, n) {
		return true
	}
	fail(w, 429, msg)
	return false
}
func validRemoteURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("ใช้ที่อยู่ HTTPS จาก Tailscale Serve")
	}
	return nil
}

func (a *App) handler(web string) http.Handler {
	m := http.NewServeMux()
	a.authRoutes(m)
	a.cameraRoutes(m)
	a.connectionRoutes(m)
	a.mediaRoutes(m)
	a.pairRoutes(m)
	a.backupRoutes(m)
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]string{"status": "ok", "mode": a.mode, "version": appVersion})
	})
	m.HandleFunc("GET /api/system", a.require(true, a.systemHandler))
	m.HandleFunc("PUT /api/system/remote", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		var b struct{ URL string }
		if !readJSON(w, r, &b) {
			return
		}
		if b.URL != "" {
			if err := validRemoteURL(b.URL); err != nil {
				fail(w, 400, err.Error())
				return
			}
		}
		if a.store.setSetting("remote_url", strings.TrimRight(b.URL, "/")) != nil {
			fail(w, 500, "บันทึกไม่สำเร็จ")
			return
		}
		ok(w)
	}))
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, 404, "ไม่พบคำสั่งนี้")
	})
	m.HandleFunc("/", staticHandler(web))
	return a.security(m)
}
func (a *App) systemHandler(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	jsonOut(w, 200, map[string]any{"mode": a.mode, "bridge": a.bridge.healthy(ctx), "uptimeSeconds": int(time.Since(a.started).Seconds()), "appMemoryMB": mem.Alloc / 1024 / 1024, "architecture": runtime.GOARCH, "events": a.store.events(), "fieldTested": false, "discovery": "เปิดเฉพาะการเชื่อมต่อที่เจ้าของเปิดใช้", "remoteURL": a.store.setting("remote_url")})
}

// Static files: hashed assets cache forever, everything else revalidates, and
// unknown extensionless routes fall back to the SPA shell. Directory listings
// and dot-files are never served.
func staticHandler(web string) http.HandlerFunc {
	index := filepath.Join(web, "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		p := path.Clean("/" + r.URL.Path)
		if strings.Contains(p, "/.") {
			http.NotFound(w, r)
			return
		}
		file := filepath.Join(web, filepath.FromSlash(p))
		info, err := os.Stat(file)
		if err != nil || info.IsDir() {
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, index)
			return
		}
		if strings.HasPrefix(p, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, file)
	}
}
func (a *App) seedDemo() error {
	names := []string{"ประตูหน้าบ้าน", "โรงจอดรถ", "ห้องนั่งเล่น", "โถงทางเดิน", "หน้าห้องทำงาน", "ห้องครัว", "สวนข้างบ้าน", "ระเบียงชั้นสอง", "ประตูหลังบ้าน", "บันได", "ห้องอเนกประสงค์", "เฉลียงหน้าบ้าน", "มุมสวน · ออฟไลน์", "คลังเก็บของ · รอยืนยัน", "ห้องอ่านหนังสือ"}
	groups := []string{"นอกบ้าน", "นอกบ้าน", "ชั้นล่าง", "ชั้นล่าง", "ชั้นสอง", "ชั้นล่าง", "นอกบ้าน", "ชั้นสอง", "นอกบ้าน", "ชั้นล่าง", "ชั้นสอง", "นอกบ้าน", "นอกบ้าน", "ชั้นล่าง", "ชั้นสอง"}
	for i, name := range names {
		vendor := "xiaomi"
		if i < 2 {
			vendor = "tapo"
		}
		ref := fmt.Sprintf("demo-%02d", i+1)
		id := cameraID(vendor, ref)
		if _, err := a.store.camera(id); err == nil {
			continue
		}
		c := demoCamera(vendor, ref, name, groups[i], i%4+1)
		c.Host = fmt.Sprintf("192.0.2.%d", i+10)
		c.PTZ = i != 14
		c.Favorite = i < 4
		if i == 12 {
			c.Status = "offline"
			c.Message = "ตัวอย่างกล้องขาดการเชื่อมต่อ"
		}
		if i == 13 {
			c.Status = "auth_expired"
			c.Message = "ตัวอย่างบัญชีที่ต้องยืนยันอีกครั้ง"
		}
		if err := a.store.saveCamera(c); err != nil {
			return err
		}
	}
	return nil
}
func demoCamera(vendor, ref, name, group string, scene int) Camera {
	return Camera{ID: cameraID(vendor, ref), Vendor: vendor, Ref: ref, Name: name, Group: group, Model: "กล้องจำลอง", Audio: true, PTZ: true, Status: "online", Verification: "demo", LastSeen: time.Now(), Source: Source{Low: fmt.Sprintf("ffmpeg:/media/scene-%d-low.mp4#input=file#video=copy#audio=copy", scene), High: fmt.Sprintf("ffmpeg:/media/scene-%d-high.mp4#input=file#video=copy#audio=copy", scene), PTZProtocol: "demo"}}
}
func (a *App) cameraRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/cameras", a.require(false, a.listCameras))
	m.HandleFunc("PUT /api/cameras/order", a.require(true, a.cameraOrderHandler))
	m.HandleFunc("PATCH /api/cameras/{id}", a.require(true, a.patchCamera))
	m.HandleFunc("POST /api/cameras/{id}/probe", a.require(true, a.probeCamera))
	m.HandleFunc("POST /api/cameras/{id}/ptz", a.require(false, a.ptzHandler))
	m.HandleFunc("POST /api/cameras/{id}/ptz/probe", a.require(true, a.ptzProbeHandler))
	m.HandleFunc("POST /api/demo/cameras", a.require(true, a.demoOnly(a.addDemoCamera)))
	m.HandleFunc("POST /api/demo/cameras/{id}/scenario", a.require(true, a.demoOnly(a.demoScenario)))
}
func (a *App) listCameras(w http.ResponseWriter, r *http.Request) {
	cs, err := a.store.cameras()
	if err != nil {
		fail(w, 500, "อ่านรายการกล้องไม่ได้")
		return
	}
	p := principalOf(r)
	if p.Cameras != nil {
		// Share links see only their cameras and never the LAN address or device id.
		scoped := []Camera{}
		for _, c := range cs {
			if p.allows(c.ID) {
				c.Host, c.Ref = "", ""
				scoped = append(scoped, c)
			}
		}
		cs = scoped
	}
	jsonOut(w, 200, cs)
}
func (a *App) patchCamera(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name          *string `json:"name"`
		Group         *string `json:"group"`
		Favorite      *bool   `json:"favorite"`
		PTZProtocol   *string `json:"ptzProtocol"`
		AudioVerified *bool   `json:"audioVerified"`
		PTZVerified   *bool   `json:"ptzVerified"`
		PlaybackMode  *string `json:"playbackMode"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	if b.Name != nil {
		*b.Name = strings.TrimSpace(*b.Name)
		if *b.Name == "" || runes(*b.Name) > 180 {
			fail(w, 400, "ชื่อกล้องต้องไม่ว่างและไม่เกิน 180 ตัวอักษร")
			return
		}
	}
	if b.Group != nil {
		*b.Group = strings.TrimSpace(*b.Group)
		if runes(*b.Group) > 120 {
			fail(w, 400, "ชื่อกลุ่มยาวเกินไป")
			return
		}
	}
	if b.PlaybackMode != nil && *b.PlaybackMode != "native" && *b.PlaybackMode != "h264" {
		fail(w, 400, "รูปแบบวิดีโอไม่ถูกต้อง")
		return
	}
	if b.PTZProtocol != nil && *b.PTZProtocol != "none" && *b.PTZProtocol != "miss-step" {
		fail(w, 400, "รูปแบบการหมุนไม่ถูกต้อง")
		return
	}
	c, err := a.store.updateCamera(r.PathValue("id"), func(c *Camera) {
		if b.Name != nil {
			c.Name = *b.Name
		}
		if b.Group != nil {
			c.Group = *b.Group
		}
		if b.Favorite != nil {
			c.Favorite = *b.Favorite
		}
		if b.PlaybackMode != nil {
			c.PlaybackMode = *b.PlaybackMode
		}
		if b.PTZProtocol != nil && c.Vendor == "xiaomi" {
			c.Source.PTZProtocol = *b.PTZProtocol
			c.PTZ = *b.PTZProtocol == "miss-step"
			c.PTZVerified = false
		}
		if b.AudioVerified != nil {
			c.AudioVerified = *b.AudioVerified
		}
		if b.PTZVerified != nil {
			c.PTZVerified = *b.PTZVerified && c.PTZ
		}
	})
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "ไม่พบกล้อง")
		return
	}
	if err != nil {
		fail(w, 500, "บันทึกไม่สำเร็จ")
		return
	}
	jsonOut(w, 200, c)
}
func (a *App) probeCamera(w http.ResponseWriter, r *http.Request) {
	c, found := a.cameraFromPath(w, r)
	if !found {
		return
	}
	if cameraUnavailable(c) {
		fail(w, 409, c.Message)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	if a.mode == "live" && c.Vendor == "tapo" {
		// Re-read advertised profiles using the account already encrypted on
		// the hub, so a profile-selection fix does not require re-entry.
		fresh, err := (&ONVIF{}).inspect(ctx, c.Host, c.Source.Username, c.Source.Password)
		if err != nil {
			fail(w, 502, onvifSetupMessage(err))
			return
		}
		if fresh.ID != c.ID {
			fail(w, 409, "กล้องที่ IP นี้มีรหัสอุปกรณ์เปลี่ยนไป กรุณาตรวจ IP ก่อนเชื่อมต่อใหม่")
			return
		}
		if err = a.store.mergeCamera(fresh); err != nil {
			fail(w, 500, "ปรับข้อมูลช่องภาพไม่สำเร็จ")
			return
		}
		if c, err = a.store.camera(c.ID); err != nil {
			fail(w, 500, "อ่านข้อมูลกล้องไม่สำเร็จ")
			return
		}
	}
	probeErr := a.bridge.ensure(ctx, c, "low")
	if probeErr == nil {
		probeErr = a.bridge.probe(ctx, streamName(c, "low"))
	}
	// Only the video result is written; a rename that landed meanwhile survives.
	c, err := a.store.updateCamera(c.ID, func(c *Camera) {
		if probeErr != nil {
			c.Status = "error"
			c.Message = "ยังเปิดภาพไม่ได้ ตรวจที่อยู่กล้อง บัญชี และรูปแบบวิดีโอ"
			return
		}
		c.Status = "online"
		c.Message = ""
		c.LastSeen = time.Now()
		if a.mode != "demo" {
			c.Verification = "stream_only"
		}
	})
	if err != nil {
		fail(w, 500, "บันทึกผลตรวจไม่สำเร็จ")
		return
	}
	jsonOut(w, 200, c)
}
func (a *App) addDemoCamera(w http.ResponseWriter, r *http.Request) {
	cs, err := a.store.cameras()
	if err != nil {
		fail(w, 500, "อ่านข้อมูลไม่สำเร็จ")
		return
	}
	c := demoCamera("xiaomi", "demo-extra-"+randomToken(6), fmt.Sprintf("กล้องใหม่ %02d", len(cs)+1), "ยังไม่จัดกลุ่ม", 1)
	if a.store.saveCamera(c) != nil {
		fail(w, 500, "เพิ่มไม่สำเร็จ")
		return
	}
	a.store.event("demo", "เพิ่มกล้องจำลองเรียบร้อย")
	jsonOut(w, 201, c)
}
func (a *App) demoScenario(w http.ResponseWriter, r *http.Request) {
	var b struct{ Status string }
	if !readJSON(w, r, &b) {
		return
	}
	if b.Status != "online" && b.Status != "offline" && b.Status != "auth_expired" {
		fail(w, 400, "สถานะไม่ถูกต้อง")
		return
	}
	a.stopCamera(r.PathValue("id"), "")
	c, err := a.store.updateCamera(r.PathValue("id"), func(c *Camera) {
		c.Status = b.Status
		c.Message = map[string]string{"offline": "ตัวอย่างกล้องขาดการเชื่อมต่อ", "auth_expired": "ตัวอย่างบัญชีที่ต้องยืนยันอีกครั้ง"}[b.Status]
	})
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "ไม่พบกล้อง")
		return
	}
	if err != nil {
		fail(w, 500, "บันทึกไม่สำเร็จ")
		return
	}
	jsonOut(w, 200, c)
}

// Workers. Registration retries until the gateway is up but never blocks the
// cleanup and scheduled-sync tickers.
func (a *App) background(ctx context.Context) {
	go a.registerAllStreams(ctx)
	tapo := time.NewTicker(2 * time.Minute)
	xiaomi := time.NewTicker(5 * time.Minute)
	clean := time.NewTicker(time.Minute)
	defer tapo.Stop()
	defer xiaomi.Stop()
	defer clean.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tapo.C:
			a.scheduledSync(ctx, "tapo", 60*time.Second)
		case <-xiaomi.C:
			a.scheduledSync(ctx, "xiaomi", 60*time.Second)
		case <-clean.C:
			a.cleanupExpired()
		}
	}
}
func (a *App) registerAllStreams(ctx context.Context) {
	// Registration is local to the gateway; never opens a camera connection by itself.
	for !a.bridge.healthy(ctx) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	cs, err := a.store.cameras()
	if err != nil {
		log.Printf("stream registration: %v", err)
		return
	}
	for _, c := range cs {
		_ = a.bridge.ensure(ctx, c, "low")
	}
}
func (a *App) scheduledSync(ctx context.Context, vendor string, timeout time.Duration) {
	if a.mode != "live" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, _ = a.syncConnections(ctx, vendor)
}
func (a *App) cleanupExpired() {
	now := time.Now()
	a.mu.Lock()
	for id, p := range a.pairs {
		if now.After(p.Expires) {
			delete(a.pairs, id)
		}
	}
	a.mu.Unlock()
	if _, err := a.store.DB.Exec("DELETE FROM sessions WHERE expires<?", now.Unix()); err != nil {
		log.Printf("session cleanup: %v", err)
	}
}
func (a *App) pairRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/pair/request", func(w http.ResponseWriter, r *http.Request) {
		if a.store.setting("owner_hash") == "" {
			fail(w, 409, "ตั้งค่าเจ้าของระบบบนมือถือก่อน")
			return
		}
		if !a.limit(w, r, "pair", 6, "ลองใหม่ในอีกหนึ่งนาที") {
			return
		}
		var b struct{ Name string }
		if !readJSON(w, r, &b) {
			return
		}
		b.Name = strings.TrimSpace(b.Name)
		if b.Name == "" || runes(b.Name) > 80 {
			fail(w, 400, "ระบุชื่อทีวี")
			return
		}
		p := &pairRequest{ID: randomToken(16), Code: strings.ToUpper(randomToken(6)), Secret: randomToken(32), Name: b.Name, Expires: time.Now().Add(5 * time.Minute)}
		a.mu.Lock()
		a.pairs[p.ID] = p
		a.mu.Unlock()
		jsonOut(w, 201, map[string]any{"id": p.ID, "code": p.Code, "secret": p.Secret, "expires": p.Expires})
	})
	m.HandleFunc("POST /api/pair/poll", func(w http.ResponseWriter, r *http.Request) {
		if !a.limit(w, r, "poll", 90, "รอสักครู่") {
			return
		}
		var b struct{ ID, Secret string }
		if !readJSON(w, r, &b) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		p := a.pairs[b.ID]
		if p == nil || time.Now().After(p.Expires) || tokenHash(b.Secret) != tokenHash(p.Secret) {
			fail(w, 404, "รหัสจับคู่หมดอายุหรือไม่ถูกต้อง")
			return
		}
		if p.Approved {
			jsonOut(w, 200, map[string]any{"approved": true, "token": p.Token})
			delete(a.pairs, p.ID)
		} else {
			jsonOut(w, 200, map[string]bool{"approved": false})
		}
	})
	m.HandleFunc("GET /api/pair/pending", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		out := []map[string]any{}
		for _, p := range a.pairs {
			if !p.Approved && time.Now().Before(p.Expires) {
				out = append(out, map[string]any{"code": p.Code, "name": p.Name, "expires": p.Expires})
			}
		}
		jsonOut(w, 200, out)
	}))
	m.HandleFunc("POST /api/pair/approve", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Code string }
		if !readJSON(w, r, &b) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, p := range a.pairs {
			if strings.EqualFold(p.Code, strings.TrimSpace(b.Code)) && !p.Approved && time.Now().Before(p.Expires) {
				tok, err := a.issueSession(w, r, p.Name, "tv", false, nil)
				if err != nil {
					fail(w, 500, "จับคู่ไม่สำเร็จ")
					return
				}
				p.Token = tok
				p.Approved = true
				a.store.event("pair", "จับคู่ทีวีเรียบร้อย")
				ok(w)
				return
			}
		}
		fail(w, 404, "ไม่พบรหัสจับคู่ หรือหมดอายุแล้ว")
	}))
}
func limitRead(r io.Reader, n int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, n+1))
	if int64(len(b)) > n {
		return nil, errors.New("response too large")
	}
	return b, e
}
func qualityOf(r *http.Request) string {
	if r.URL.Query().Get("quality") == "high" {
		return "high"
	}
	return "low"
}
