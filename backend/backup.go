package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/scrypt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type backupCamera struct {
	Camera Camera `json:"camera"`
	Source Source `json:"source"`
}
type backupConnection struct {
	Connection Connection `json:"connection"`
	Username   string     `json:"username"`
	Password   string     `json:"password"`
}
type backupData struct {
	Version     int                `json:"version"`
	Mode        string             `json:"mode"`
	Cameras     []backupCamera     `json:"cameras"`
	Connections []backupConnection `json:"connections"`
	Gateway     []byte             `json:"gateway"`
	RemoteURL   string             `json:"remoteURL"`
}

func backupEncrypt(password string, raw []byte) ([]byte, error) {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key, e := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
	if e != nil {
		return nil, e
	}
	sealed, e := seal(key, raw)
	if e != nil {
		return nil, e
	}
	out := append([]byte("CAMHUB01"), salt...)
	return append(out, sealed...), nil
}
func backupDecrypt(password string, raw []byte) ([]byte, error) {
	if len(raw) < 52 || string(raw[:8]) != "CAMHUB01" {
		return nil, errors.New("invalid backup")
	}
	key, e := scrypt.Key([]byte(password), raw[8:24], 32768, 8, 1, 32)
	if e != nil {
		return nil, e
	}
	return unseal(key, raw[24:])
}
func decodeBackup(password, file string) (backupData, error) {
	var in backupData
	raw, e := base64.StdEncoding.DecodeString(file)
	if e != nil {
		return in, e
	}
	if raw, e = backupDecrypt(password, raw); e != nil {
		return in, e
	}
	return in, json.Unmarshal(raw, &in)
}

// A restored file is owner-supplied input: every source must look like what
// discovery itself would have produced for this mode, so a crafted backup
// cannot point the gateway at an arbitrary host or run commands.
func validateBackup(in backupData, mode string) error {
	if in.Version != 1 || in.Mode != mode {
		return errors.New("เวอร์ชันหรือโหมดของไฟล์สำรองไม่ตรงกับระบบนี้")
	}
	if in.RemoteURL != "" && validRemoteURL(in.RemoteURL) != nil {
		return errors.New("ที่อยู่ระยะไกลในไฟล์สำรองไม่ถูกต้อง")
	}
	seen := map[string]bool{}
	for _, entry := range in.Cameras {
		if entry.Camera.ID == "" || seen[entry.Camera.ID] {
			return errors.New("รายการกล้องในไฟล์สำรองซ้ำหรือไม่มีรหัส")
		}
		seen[entry.Camera.ID] = true
		if err := validBackupSource(mode, entry.Camera, entry.Source); err != nil {
			return err
		}
	}
	seen = map[string]bool{}
	for _, entry := range in.Connections {
		c := entry.Connection
		if c.ID == "" || seen[c.ID] || (c.Vendor != "tapo" && c.Vendor != "xiaomi") || (c.Vendor == "xiaomi" && (!regions[c.Region] || !accountPattern.MatchString(c.Account))) {
			return errors.New("บัญชีในไฟล์สำรองไม่ถูกต้อง")
		}
		seen[c.ID] = true
	}
	return nil
}
func validBackupSource(mode string, c Camera, s Source) error {
	bad := errors.New("แหล่งวิดีโอในไฟล์สำรองไม่ถูกต้อง")
	if s.Low == "" {
		return bad
	}
	streams := []string{s.Low, s.High}
	if mode == "demo" {
		for _, raw := range streams {
			if raw != "" && (!strings.HasPrefix(raw, "ffmpeg:/media/") || strings.Contains(raw, "exec") || strings.Contains(raw, "|")) {
				return bad
			}
		}
		return nil
	}
	if validateCameraHost(c.Host) != nil {
		return bad
	}
	switch c.Vendor {
	case "tapo":
		for _, raw := range streams {
			if u, e := url.Parse(raw); raw != "" && (e != nil || u.Scheme != "rtsp" || u.Hostname() != c.Host) {
				return bad
			}
		}
		for _, raw := range []string{s.DeviceURL, s.MediaURL, s.PTZURL} {
			if raw != "" && !sameCameraURL(raw, c.Host) {
				return bad
			}
		}
		if s.PTZProtocol != "" && s.PTZProtocol != "onvif" {
			return bad
		}
	case "xiaomi":
		for _, raw := range streams {
			if u, e := url.Parse(raw); raw != "" && (e != nil || u.Scheme != "xiaomi" || u.Query().Get("did") == "" || validateCameraHost(u.Hostname()) != nil) {
				return bad
			}
		}
		if s.PTZProtocol != "" && s.PTZProtocol != "none" && s.PTZProtocol != "miss-step" {
			return bad
		}
	default:
		return bad
	}
	return nil
}

// applyBackup replaces cameras and connections atomically.
func (s *Store) applyBackup(in backupData) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM cameras; DELETE FROM connections;"); err != nil {
		return err
	}
	for _, entry := range in.Cameras {
		c := entry.Camera
		c.Source = entry.Source
		if err = s.upsertCamera(tx, c); err != nil {
			return err
		}
	}
	for _, entry := range in.Connections {
		c := entry.Connection
		c.Username, c.Password = entry.Username, entry.Password
		if err = s.upsertConnection(tx, c); err != nil {
			return err
		}
	}
	if err = upsertSetting(tx, "remote_url", strings.TrimRight(in.RemoteURL, "/")); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *App) backupRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/backup", a.require(true, a.exportBackup))
	m.HandleFunc("POST /api/backup/restore", a.require(true, a.restoreBackup))
}
func (a *App) exportBackup(w http.ResponseWriter, r *http.Request) {
	if !a.limit(w, r, "backup", 3, "รอสักครู่แล้วลองใหม่") {
		return
	}
	var b struct{ Password string }
	if !readJSON(w, r, &b) {
		return
	}
	if len(b.Password) < 12 || len(b.Password) > 256 {
		fail(w, 400, "ตั้งรหัสสำรองอย่างน้อย 12 ตัวอักษร")
		return
	}
	cs, e := a.store.cameras()
	if e != nil {
		fail(w, 500, "อ่านข้อมูลไม่ได้")
		return
	}
	conns, e := a.store.connections()
	if e != nil {
		fail(w, 500, "อ่านข้อมูลไม่ได้")
		return
	}
	out := backupData{Version: 1, Mode: a.mode, RemoteURL: a.store.setting("remote_url")}
	for _, c := range cs {
		out.Cameras = append(out.Cameras, backupCamera{c, c.Source})
	}
	for _, c := range conns {
		out.Connections = append(out.Connections, backupConnection{c, c.Username, c.Password})
	}
	if a.cfg.gatewayConfig != "" {
		if out.Gateway, e = os.ReadFile(a.cfg.gatewayConfig); e != nil {
			fail(w, 500, "อ่านการตั้งค่าตัวกลางไม่ได้ จึงยังสร้างไฟล์สำรองครบชุดไม่ได้")
			return
		}
	}
	raw, e := json.Marshal(out)
	if e != nil {
		fail(w, 500, "สำรองไม่สำเร็จ")
		return
	}
	enc, e := backupEncrypt(b.Password, raw)
	if e != nil {
		fail(w, 500, "เข้ารหัสไม่สำเร็จ")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="camerahub-`+a.mode+`.cambackup"`)
	_, _ = w.Write(enc)
}
func (a *App) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if !a.limit(w, r, "restore", 3, "รอสักครู่แล้วลองใหม่") {
		return
	}
	var b struct{ Password, File, Confirm string }
	if !readJSON(w, r, &b) {
		return
	}
	if b.Confirm != "RESTORE" || len(b.Password) > 256 {
		fail(w, 400, "ยืนยันด้วย RESTORE")
		return
	}
	in, e := decodeBackup(b.Password, b.File)
	if e != nil {
		fail(w, 400, "รหัสสำรองหรือไฟล์ไม่ถูกต้อง")
		return
	}
	if e = validateBackup(in, a.mode); e != nil {
		fail(w, 400, e.Error())
		return
	}
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	a.stopAll()
	// The database commits first; a gateway file failure must not leave the
	// registry rolled back while the gateway already uses the new config.
	if e = a.store.applyBackup(in); e != nil {
		fail(w, 500, "กู้คืนฐานข้อมูลไม่สำเร็จ กรุณารีสตาร์ตแล้วตรวจสถานะ")
		return
	}
	a.bridge.resetCache()
	if a.writeRelayHosts() != nil {
		a.store.event("relay", "อัปเดตรายการกล้องสำหรับตัวส่งข้อมูลไม่สำเร็จ")
	}
	if len(in.Gateway) > 0 && a.cfg.gatewayConfig != "" {
		temp := filepath.Join(filepath.Dir(a.cfg.gatewayConfig), "restore.tmp")
		if os.WriteFile(temp, in.Gateway, 0600) != nil || os.Rename(temp, a.cfg.gatewayConfig) != nil {
			a.store.event("restore", "กู้คืนฐานข้อมูลแล้ว แต่เขียนการตั้งค่าตัวกลางไม่สำเร็จ")
			fail(w, 500, "กู้คืนข้อมูลกล้องแล้ว แต่กู้คืนการตั้งค่าตัวกลางไม่สำเร็จ")
			return
		}
	}
	a.store.event("restore", "กู้คืนข้อมูลแล้ว ต้องรีสตาร์ตตัวกลางวิดีโอ")
	jsonOut(w, 200, map[string]any{"ok": true, "restartGateway": len(in.Gateway) > 0})
}
