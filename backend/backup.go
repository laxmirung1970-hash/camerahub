package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/scrypt"
	"net/http"
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
func (a *App) backupRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/backup", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		if !a.rateOK(r, "backup", 3) {
			fail(w, 429, "รอสักครู่แล้วลองใหม่")
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
		path := env("GATEWAY_CONFIG", "")
		if path != "" {
			out.Gateway, e = os.ReadFile(path)
			if e != nil {
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
	}))
	m.HandleFunc("POST /api/backup/restore", a.require(true, func(w http.ResponseWriter, r *http.Request) {
		if !a.rateOK(r, "restore", 3) {
			fail(w, 429, "รอสักครู่แล้วลองใหม่")
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
		file, e := base64.StdEncoding.DecodeString(b.File)
		if e != nil {
			fail(w, 400, "ไฟล์ไม่ถูกต้อง")
			return
		}
		raw, e := backupDecrypt(b.Password, file)
		if e != nil {
			fail(w, 400, "รหัสสำรองหรือไฟล์ไม่ถูกต้อง")
			return
		}
		var in backupData
		if json.Unmarshal(raw, &in) != nil || in.Version != 1 || in.Mode != a.mode {
			fail(w, 400, "เวอร์ชันหรือโหมดของไฟล์สำรองไม่ตรงกับระบบนี้")
			return
		}
		if a.mode == "live" {
			for _, c := range in.Cameras {
				if c.Camera.Vendor != "tapo" && c.Camera.Vendor != "xiaomi" {
					fail(w, 400, "ชนิดกล้องไม่ถูกต้อง")
					return
				}
			}
		}
		a.syncMu.Lock()
		defer a.syncMu.Unlock()
		a.stopAll()
		tx, e := a.store.DB.Begin()
		if e != nil {
			fail(w, 500, "เริ่มกู้คืนไม่สำเร็จ")
			return
		}
		defer tx.Rollback()
		if _, e = tx.Exec("DELETE FROM cameras; DELETE FROM connections;"); e != nil {
			fail(w, 500, "กู้คืนไม่สำเร็จ")
			return
		}
		for _, entry := range in.Cameras {
			p, _ := json.Marshal(entry.Camera)
			s, _ := json.Marshal(entry.Source)
			sec, err := seal(a.store.Key, s)
			if err == nil {
				_, err = tx.Exec("INSERT INTO cameras VALUES(?,?,?)", entry.Camera.ID, p, sec)
			}
			if err != nil {
				fail(w, 500, "กู้คืนข้อมูลกล้องไม่สำเร็จ")
				return
			}
		}
		for _, entry := range in.Connections {
			p, _ := json.Marshal(entry.Connection)
			s, _ := json.Marshal([2]string{entry.Username, entry.Password})
			sec, err := seal(a.store.Key, s)
			if err == nil {
				_, err = tx.Exec("INSERT INTO connections VALUES(?,?,?)", entry.Connection.ID, p, sec)
			}
			if err != nil {
				fail(w, 500, "กู้คืนบัญชีไม่สำเร็จ")
				return
			}
		}
		path := env("GATEWAY_CONFIG", "")
		if len(in.Gateway) > 0 && path != "" {
			temp := filepath.Join(filepath.Dir(path), "restore.tmp")
			if os.WriteFile(temp, in.Gateway, 0600) != nil || os.Rename(temp, path) != nil {
				fail(w, 500, "กู้คืนการตั้งค่าตัวกลางไม่สำเร็จ")
				return
			}
		}
		if tx.Commit() != nil {
			fail(w, 500, "กู้คืนฐานข้อมูลไม่สำเร็จ กรุณารีสตาร์ตแล้วตรวจสถานะ")
			return
		}
		_ = a.store.setSetting("remote_url", strings.TrimRight(in.RemoteURL, "/"))
		a.bridge.mu.Lock()
		a.bridge.registered = nil
		a.bridge.mu.Unlock()
		a.store.event("restore", "กู้คืนข้อมูลแล้ว ต้องรีสตาร์ตตัวกลางวิดีโอ")
		jsonOut(w, 200, map[string]any{"ok": true, "restartGateway": len(in.Gateway) > 0})
	}))
}
