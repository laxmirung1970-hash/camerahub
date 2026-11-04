package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var regions = map[string]bool{"cn": true, "sg": true, "de": true, "us": true, "ru": true, "i2": true}
var accountPattern = regexp.MustCompile(`^[0-9]{3,24}$`)

func (a *App) connectionRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/connections", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		cs, e := a.store.connections()
		if e != nil {
			fail(w, 500, "อ่านการเชื่อมต่อไม่สำเร็จ")
			return
		}
		jsonOut(w, 200, cs)
	}))
	m.HandleFunc("POST /api/connections", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		if a.mode != "live" {
			fail(w, 409, "โหมดสาธิตไม่รับบัญชีหรือเชื่อมกล้องจริง")
			return
		}
		var b struct {
			ID, Vendor, Name, Account, Region, Username, Password string
			Enabled                                               bool
		}
		if !readJSON(w, r, &b) {
			return
		}
		if b.Vendor != "tapo" && b.Vendor != "xiaomi" {
			fail(w, 400, "เลือก Tapo หรือ Xiaomi")
			return
		}
		if len(b.Name) > 120 || len(b.Username) > 160 || len(b.Password) > 256 {
			fail(w, 400, "ข้อมูลยาวเกินไป")
			return
		}
		if b.Vendor == "xiaomi" && (!regions[b.Region] || !accountPattern.MatchString(b.Account)) {
			fail(w, 400, "เลือก Region และรหัสบัญชี Xiaomi แบบตัวเลข")
			return
		}
		c := Connection{ID: cameraID("connection", b.Vendor+":"+b.Account), Vendor: b.Vendor, Name: b.Name, Account: b.Account, Region: b.Region, Enabled: b.Enabled, Username: b.Username, Password: b.Password, Status: "pending"}
		if b.ID != "" {
			old, _ := a.store.connections()
			found := false
			for _, o := range old {
				if o.ID == b.ID && o.Vendor == b.Vendor {
					c.ID = o.ID
					c.LastSync = o.LastSync
					found = true
					if b.Password == "" {
						c.Password = o.Password
						c.Username = o.Username
					}
					break
				}
			}
			if !found {
				fail(w, 404, "ไม่พบการเชื่อมต่อ")
				return
			}
		}
		if c.Name == "" {
			c.Name = strings.ToUpper(b.Vendor)
		}
		if c.Vendor == "tapo" && (c.Username == "" || c.Password == "") {
			fail(w, 400, "ระบุ Camera Account จากแอป Tapo")
			return
		}
		if e := a.store.saveConnection(c); e != nil {
			fail(w, 500, "บันทึกไม่สำเร็จ")
			return
		}
		a.store.event("connection", "บันทึกการเชื่อมต่อ "+c.Vendor)
		jsonOut(w, 201, c)
	}))
	m.HandleFunc("POST /api/connections/{id}/toggle", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Enabled bool }
		if !readJSON(w, r, &b) {
			return
		}
		cs, e := a.store.connections()
		if e != nil {
			fail(w, 500, "อ่านข้อมูลไม่สำเร็จ")
			return
		}
		for _, c := range cs {
			if c.ID == r.PathValue("id") {
				c.Enabled = b.Enabled
				if e = a.store.saveConnection(c); e != nil {
					fail(w, 500, "บันทึกไม่สำเร็จ")
					return
				}
				jsonOut(w, 200, c)
				return
			}
		}
		fail(w, 404, "ไม่พบการเชื่อมต่อ")
	}))
	m.HandleFunc("POST /api/discover", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		vendor := r.URL.Query().Get("vendor")
		if vendor != "" && vendor != "xiaomi" && vendor != "tapo" {
			fail(w, 400, "ระบบกล้องที่เลือกไม่ถูกต้อง")
			return
		}
		if a.mode == "demo" {
			cs, _ := a.store.cameras()
			a.store.event("demo", "ซิงก์กล้องจำลองแล้ว ไม่ได้ค้นหาเครือข่ายจริง")
			jsonOut(w, 200, map[string]any{"found": len(cs), "simulated": true, "message": "อัปเดตรายการสาธิตแล้ว ไม่มีการค้นหาเครือข่ายจริง"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		count, e := a.syncConnections(ctx, vendor)
		if e != nil {
			fail(w, 502, e.Error())
			return
		}
		jsonOut(w, 200, map[string]any{"found": count, "message": fmt.Sprintf("ค้นหาเสร็จแล้ว พบ %d รายการ กล้องที่พบใหม่ยังต้องตรวจภาพ เสียง และการหมุน", count)})
	}))
	m.HandleFunc("POST /api/cameras/tapo", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		if a.mode != "live" {
			fail(w, 409, "โหมดสาธิตไม่เชื่อมกล้องจริง")
			return
		}
		var b struct{ Host, Username, Password, Name string }
		if !readJSON(w, r, &b) {
			return
		}
		if e := validateCameraHost(b.Host); e != nil {
			fail(w, 400, e.Error())
			return
		}
		if len(b.Username) > 160 || len(b.Password) > 256 || len(b.Name) > 180 {
			fail(w, 400, "ข้อมูลยาวเกินไป")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		c, e := (&ONVIF{}).inspect(ctx, b.Host, b.Username, b.Password)
		if e != nil {
			var response *onvifResponseError
			if errors.As(e, &response) && response.Auth {
				source := (&url.URL{Scheme: "rtsp", Host: net.JoinHostPort(b.Host, "554"), Path: "/stream1", User: url.UserPassword(b.Username, b.Password)}).String()
				check, stopCheck := context.WithTimeout(ctx, 10*time.Second)
				result := a.bridge.checkRTSP(check, source)
				stopCheck()
				if result == "accepted" {
					fail(w, 502, "บัญชีผ่านการเชื่อม RTSP แล้ว แต่ ONVIF ยังปฏิเสธ จึงยังไม่เพิ่มกล้อง ต้องตรวจความเข้ากันได้ของ ONVIF ต่อ")
					return
				}
				if result == "rejected" {
					fail(w, 502, "ทั้ง RTSP และ ONVIF ปฏิเสธชื่อผู้ใช้/รหัสผ่านคู่นี้ ตรวจ Username ที่แสดงหลัง Verify ใน Tapo และกรอกรหัส Camera Account อีกครั้ง")
					return
				}
			}
			fail(w, 502, onvifSetupMessage(e))
			return
		}
		if b.Name != "" {
			c.Name = b.Name
		}
		if a.store.mergeCamera(c) != nil {
			fail(w, 500, "บันทึกกล้องไม่สำเร็จ")
			return
		}
		jsonOut(w, 201, c)
	}))
	m.HandleFunc("GET /api/xiaomi/accounts", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		if a.mode != "live" {
			jsonOut(w, 200, []string{})
			return
		}
		raw, status, e := a.bridge.request(r.Context(), "GET", "/api/xiaomi", nil)
		if e != nil || status != 200 {
			fail(w, 502, "ตัวกลาง Xiaomi ยังไม่พร้อม")
			return
		}
		var ids []string
		if json.Unmarshal(raw, &ids) != nil {
			fail(w, 502, "อ่านบัญชีไม่ได้")
			return
		}
		jsonOut(w, 200, ids)
	}))
	m.HandleFunc("POST /api/xiaomi/auth", a.require(true, a.xiaomiAuth))
}
func (a *App) xiaomiAuth(w http.ResponseWriter, r *http.Request) {
	if a.mode != "live" {
		fail(w, 409, "โหมดสาธิตไม่รับรหัสผ่าน Xiaomi")
		return
	}
	var b struct{ Username, Password, Captcha, Verify string }
	if !readJSON(w, r, &b) {
		return
	}
	if len(b.Username) > 160 || len(b.Password) > 256 || len(b.Captcha) > 20 || len(b.Verify) > 20 {
		fail(w, 400, "ข้อมูลไม่ถูกต้อง")
		return
	}
	a.xiaomiMu.Lock()
	defer a.xiaomiMu.Unlock()
	form := url.Values{}
	switch {
	case b.Username != "" && b.Password != "":
		form.Set("username", b.Username)
		form.Set("password", b.Password)
		a.authPhase = "login"
	case b.Captcha != "":
		if a.authPhase != "captcha" || time.Now().After(a.authDeadline) {
			fail(w, 409, "เริ่มเข้าสู่ระบบ Xiaomi ใหม่ก่อน")
			return
		}
		form.Set("captcha", b.Captcha)
	case b.Verify != "":
		if a.authPhase != "verify" || time.Now().After(a.authDeadline) {
			fail(w, 409, "เริ่มเข้าสู่ระบบ Xiaomi ใหม่ก่อน")
			return
		}
		form.Set("verify", b.Verify)
	default:
		fail(w, 400, "ระบุบัญชีและรหัสผ่าน หรือรหัสยืนยัน")
		return
	}
	raw, status, e := a.bridge.request(r.Context(), "POST", "/api/xiaomi", strings.NewReader(form.Encode()))
	if e != nil {
		a.authPhase = ""
		message := "ติดต่อบริการ Xiaomi ผ่านเครื่องกลางไม่สำเร็จหรือหมดเวลา กรุณาตรวจอินเทอร์เน็ตแล้วลองอีกครั้ง"
		a.store.event("xiaomi_auth", message)
		fail(w, 502, message)
		return
	}
	if status == 401 {
		var challenge struct {
			Captcha string `json:"captcha"`
			Phone   string `json:"verify_phone"`
			Email   string `json:"verify_email"`
		}
		if json.Unmarshal(raw, &challenge) != nil {
			fail(w, 502, "อ่านขั้นตอนยืนยันไม่สำเร็จ")
			return
		}
		a.authDeadline = time.Now().Add(10 * time.Minute)
		if challenge.Captcha != "" {
			a.authPhase = "captcha"
			jsonOut(w, 200, map[string]string{"step": "captcha", "captcha": challenge.Captcha})
			return
		}
		if challenge.Phone != "" || challenge.Email != "" {
			a.authPhase = "verify"
			jsonOut(w, 200, map[string]string{"step": "verify", "destination": challenge.Phone + challenge.Email})
			return
		}
	}
	if status >= 300 {
		a.authPhase = ""
		message := xiaomiAuthFailure(raw, status)
		a.store.event("xiaomi_auth", message)
		fail(w, 502, message)
		return
	}
	a.authPhase = ""
	raw, status, e = a.bridge.request(r.Context(), "GET", "/api/xiaomi", nil)
	var ids []string
	if e != nil || status != 200 || json.Unmarshal(raw, &ids) != nil {
		fail(w, 502, "เข้าสู่ระบบแล้ว แต่อ่านหมายเลขบัญชีไม่สำเร็จ")
		return
	}
	if len(ids) == 0 {
		fail(w, 502, "ตัวกลางยังไม่มีบัญชี Xiaomi ที่เชื่อมสำเร็จ กรุณาเริ่มเชื่อมบัญชีใหม่")
		return
	}
	jsonOut(w, 200, map[string]any{"step": "done", "accounts": ids})
}

// Do not forward upstream responses: they can contain tokens, cookies and URLs.
// Only a numeric Xiaomi error code and fixed diagnostic categories are public.
func xiaomiAuthFailure(raw []byte, status int) string {
	message := "Xiaomi ยังไม่อนุญาตให้เชื่อมบัญชี (HTTP " + fmt.Sprint(status) + ")"
	if start := bytes.IndexByte(raw, '{'); start >= 0 {
		var reply struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(bytes.TrimSpace(raw[start:]), &reply) == nil && reply.Code > 0 && reply.Code <= 1000000 {
			return message + " · รหัส Xiaomi " + fmt.Sprint(reply.Code) + " กรุณาตรวจการเข้าสู่บัญชี Xiaomi เดิมก่อนลองใหม่"
		}
	}
	text := strings.ToLower(string(raw))
	switch {
	case strings.Contains(text, "timeout"), strings.Contains(text, "deadline exceeded"):
		return "การติดต่อ Xiaomi หมดเวลา กรุณาตรวจอินเทอร์เน็ตของเครื่องกลางแล้วลองอีกครั้ง"
	case strings.Contains(text, "no such host"):
		return "เครื่องกลางค้นหาที่อยู่บริการ Xiaomi ไม่สำเร็จ กรุณาตรวจ DNS และอินเทอร์เน็ต"
	case strings.Contains(text, "x509"), strings.Contains(text, "tls handshake"):
		return "ตรวจสอบการเชื่อมต่อที่เข้ารหัสกับ Xiaomi ไม่สำเร็จ กรุณาตรวจเวลาและใบรับรองของเครื่องกลาง"
	}
	return message + " กรุณาตรวจบัญชี รหัสผ่าน หรือขั้นตอนยืนยันใน Xiaomi"
}
func (a *App) syncConnections(ctx context.Context, vendor string) (int, error) {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	defer func() {
		if a.writeRelayHosts() != nil {
			a.store.event("relay", "อัปเดตรายการกล้องสำหรับตัวส่งข้อมูลไม่สำเร็จ")
		}
	}()
	conns, e := a.store.connections()
	if e != nil {
		return 0, e
	}
	count := 0
	enabled := 0
	failed := false
	for _, conn := range conns {
		if !conn.Enabled || (vendor != "" && conn.Vendor != vendor) {
			continue
		}
		enabled++
		var cs []Camera
		var err error
		if conn.Vendor == "xiaomi" {
			cs, err = a.discoverXiaomi(ctx, conn)
		} else {
			cs, err = a.discoverTapo(ctx, conn)
		}
		conn.LastSync = time.Now()
		if err != nil {
			failed = true
			conn.Status = "error"
			conn.Message = "ซิงก์ไม่ได้ ตรวจบัญชี Region และเครือข่าย"
			_ = a.store.saveConnection(conn)
			continue
		}
		conn.Status = "ready"
		conn.Message = fmt.Sprintf("พบ %d ช่องภาพ รายการเดิมยังคงอยู่", len(cs))
		for i := range cs {
			cs[i].Source.Connection = conn.ID
		}
		merged, mergeErr := a.store.mergeDiscoveredCameras(cs)
		if mergeErr != nil {
			failed = true
			conn.Status = "error"
			conn.Message = "บันทึกรายการช่องภาพไม่สำเร็จ ลองซิงก์ใหม่"
		}
		count += merged
		_ = a.store.saveConnection(conn)
	}
	if enabled == 0 {
		return 0, errors.New("ยังไม่มีการเชื่อมต่อที่เปิดใช้ เพิ่มบัญชีในหน้าตั้งค่าก่อน")
	}
	if failed {
		a.store.event("sync", "มีการเชื่อมต่อที่ซิงก์ไม่สำเร็จ ตรวจหน้าตั้งค่า")
		return count, errors.New("ซิงก์ได้บางส่วนหรือไม่สำเร็จ ตรวจสถานะในหน้าตั้งค่า")
	}
	a.store.event("sync", fmt.Sprintf("ซิงก์ช่องภาพแล้ว %d ช่อง", count))
	return count, nil
}

type xiaomiSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (a *App) discoverXiaomi(ctx context.Context, conn Connection) ([]Camera, error) {
	a.xiaomiMu.Lock()
	defer a.xiaomiMu.Unlock()
	raw, status, e := a.bridge.request(ctx, "GET", "/api/xiaomi?id="+url.QueryEscape(conn.Account)+"&region="+url.QueryEscape(conn.Region), nil)
	if e != nil || status != 200 {
		return nil, errors.New("Xiaomi cloud unavailable")
	}
	return parseXiaomiSources(raw, conn)
}
func parseXiaomiSources(raw []byte, conn Connection) ([]Camera, error) {
	var sources []xiaomiSource
	// go2rtc returns {"sources":[...]}; retain support for older bare lists.
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		if e := json.Unmarshal(raw, &sources); e != nil {
			return nil, e
		}
	} else {
		var reply struct {
			Sources json.RawMessage `json:"sources"`
		}
		if e := json.Unmarshal(raw, &reply); e != nil {
			return nil, e
		}
		if len(reply.Sources) == 0 || !bytes.HasPrefix(bytes.TrimSpace(reply.Sources), []byte("[")) {
			return nil, errors.New("invalid Xiaomi sources response")
		}
		if e := json.Unmarshal(reply.Sources, &sources); e != nil {
			return nil, e
		}
	}
	out := []Camera{}
	seen := map[string]bool{}
	for _, s := range sources {
		u, e := url.Parse(s.URL)
		if e != nil || u.Scheme != "xiaomi" || validateCameraHost(u.Hostname()) != nil {
			continue
		}
		ref := u.Query().Get("did")
		model := u.Query().Get("model")
		if ref == "" {
			continue
		}
		low := *u
		q := low.Query()
		q.Set("subtype", "sd")
		low.RawQuery = q.Encode()
		high := *u
		q = high.Query()
		q.Set("subtype", "hd")
		high.RawQuery = q.Encode()
		ptz := strings.HasPrefix(model, "chuangmi.camera.026c02") || strings.HasPrefix(model, "chuangmi.camera.026c05")
		protocol := "none"
		if ptz {
			protocol = "miss-step"
		}
		c := Camera{ID: cameraID("xiaomi", conn.Account+":"+ref), Vendor: "xiaomi", Ref: ref, Name: s.Name, Group: "ยังไม่จัดกลุ่ม", Model: model, Host: u.Hostname(), Region: conn.Region, Status: "pending", Message: "พบกล้องแล้ว ยังไม่ตรวจภาพ เสียง และการหมุน", Audio: true, PTZ: ptz, Verification: "untested", LastSeen: time.Now(), Source: Source{Low: low.String(), High: high.String(), PTZProtocol: protocol}}
		lenses, err := xiaomiLenses(c)
		if err != nil {
			return nil, err
		}
		for _, lens := range lenses {
			if !seen[lens.ID] {
				out = append(out, lens)
				seen[lens.ID] = true
			}
		}
	}
	return out, nil
}
func (a *App) discoverTapo(ctx context.Context, conn Connection) ([]Camera, error) {
	hosts, e := discoverONVIF(ctx)
	if e != nil {
		return nil, e
	}
	out := []Camera{}
	for _, host := range hosts {
		c, err := (&ONVIF{}).inspect(ctx, host, conn.Username, conn.Password)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}
