package main

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func (a *App) ptzHandler(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Direction string `json:"direction"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	switch b.Direction {
	case "left", "right", "up", "down", "stop":
	default:
		fail(w, 400, "ทิศทางไม่ถูกต้อง")
		return
	}
	c, e := a.store.camera(r.PathValue("id"))
	if e != nil {
		fail(w, 404, "ไม่พบกล้อง")
		return
	}
	owner := principalID(a, r)
	if b.Direction == "stop" {
		if e = a.stopCamera(c.ID, owner); e != nil {
			fail(w, 502, "ส่งคำสั่งหยุดไม่สำเร็จ กล้อง ONVIF มีเวลาหยุดสำรองในตัว 1 วินาที")
			return
		}
		jsonOut(w, 200, map[string]any{"ok": true, "simulated": a.mode == "demo", "method": c.Source.PTZProtocol})
		return
	}
	if !c.PTZ {
		fail(w, 409, "กล้องนี้ยังไม่รองรับหรือยังไม่เปิดใช้การหมุน")
		return
	}
	if c.Status == "offline" || c.Status == "auth_expired" {
		fail(w, 409, "กล้องยังไม่พร้อมเชื่อมต่อ")
		return
	}
	if !a.rateOK(r, "ptz", 300) {
		fail(w, 429, "ส่งคำสั่งถี่เกินไป")
		return
	}
	a.mu.Lock()
	lease := a.leases[c.ID]
	if lease != nil && lease.Owner != owner {
		a.mu.Unlock()
		fail(w, 409, "กล้องกำลังถูกควบคุมจากอีกอุปกรณ์")
		return
	}
	if lease != nil {
		lease.Timer.Stop()
	}
	current := &ptzLease{Owner: owner, Camera: c}
	current.Timer = time.AfterFunc(800*time.Millisecond, func() {
		a.mu.Lock()
		if a.leases[c.ID] != current {
			a.mu.Unlock()
			return
		}
		delete(a.leases, c.ID)
		a.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.move(ctx, c, "stop")
	})
	a.leases[c.ID] = current
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	e = a.move(ctx, c, b.Direction)
	if e != nil {
		_ = a.stopCamera(c.ID, owner)
		fail(w, 502, "กล้องไม่ยอมรับคำสั่งหมุน ตรวจความเข้ากันได้และเปิดภาพก่อนควบคุม")
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "simulated": a.mode == "demo", "method": c.Source.PTZProtocol})
}
func (a *App) move(ctx context.Context, c Camera, d string) error {
	if a.mode == "demo" {
		a.store.event("demo", "จำลองคำสั่งหมุนกล้อง: "+d)
		return nil
	}
	switch c.Source.PTZProtocol {
	case "onvif":
		return (&ONVIF{}).move(ctx, c, d)
	case "miss-step":
		if d == "stop" {
			return nil
		}
		op := map[string]int{"left": 1, "right": 2, "up": 3, "down": 4}[d]
		if op == 0 {
			return errors.New("invalid direction")
		}
		_, err := a.xiaomiMotor(ctx, c, op)
		return err
	default:
		return errors.New("unsupported PTZ")
	}
}
func (a *App) stopCamera(id, owner string) error {
	a.mu.Lock()
	lease := a.leases[id]
	if lease == nil || (owner != "" && lease.Owner != owner) {
		a.mu.Unlock()
		return nil
	}
	lease.Timer.Stop()
	delete(a.leases, id)
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return a.move(ctx, lease.Camera, "stop")
}
func (a *App) stopPrincipal(owner string) {
	a.mu.Lock()
	ids := []string{}
	for id, l := range a.leases {
		if l.Owner == owner {
			ids = append(ids, id)
		}
	}
	a.mu.Unlock()
	for _, id := range ids {
		_ = a.stopCamera(id, owner)
	}
}
func (a *App) stopAll() {
	a.mu.Lock()
	ids := []string{}
	for id := range a.leases {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	for _, id := range ids {
		_ = a.stopCamera(id, "")
	}
}
