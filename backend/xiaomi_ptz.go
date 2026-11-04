package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type motorReply struct {
	Ret       *int     `json:"ret"`
	Angle     *float64 `json:"angle"`
	Elevation *float64 `json:"elevation"`
}

func (a *App) xiaomiMotor(ctx context.Context, c Camera, operation int) (motorReply, error) {
	q := url.Values{"src": {streamName(c, "low")}, "cmd": {"0x112"}, "data": {`{"operation":` + strconv.Itoa(operation) + `}`}}
	raw, status, err := a.bridge.request(ctx, "POST", "/api/xiaomi/command?"+q.Encode(), nil)
	if err == nil && status == http.StatusConflict {
		q.Set("src", streamName(c, "high"))
		raw, status, err = a.bridge.request(ctx, "POST", "/api/xiaomi/command?"+q.Encode(), nil)
	}
	if err != nil || status != 200 {
		return motorReply{}, errors.New("Xiaomi control unavailable; open video first")
	}
	var envelope struct {
		Reply string `json:"reply"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return motorReply{}, errors.New("invalid Xiaomi envelope")
	}
	var reply motorReply
	if json.Unmarshal(bytes.TrimRight([]byte(envelope.Reply), "\x00"), &reply) != nil || reply.Ret == nil {
		return reply, errors.New("invalid Xiaomi motor response")
	}
	// Some firmware reports -5 at a completed movement, with valid coordinates.
	if *reply.Ret != 0 && !(*reply.Ret == -5 && validMotorPosition(reply)) {
		return reply, errors.New("camera rejected motor command")
	}
	return reply, nil
}
func validMotorPosition(reply motorReply) bool {
	return reply.Angle != nil && reply.Elevation != nil && *reply.Angle >= 0 && *reply.Angle <= 101 && *reply.Elevation >= 0 && *reply.Elevation <= 101
}

func (a *App) ptzProbeHandler(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.camera(r.PathValue("id"))
	if err != nil {
		fail(w, 404, "ไม่พบกล้อง")
		return
	}
	if a.mode == "demo" {
		jsonOut(w, 200, map[string]any{"message": "กล้องจำลอง ไม่มีมอเตอร์จริง", "supported": c.PTZ})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if c.Vendor != "xiaomi" {
		jsonOut(w, 200, map[string]any{"supported": c.PTZ, "message": "กล้องใช้ ONVIF · กดทิศทางและตรวจภาพเพื่อยืนยันการเคลื่อนไหว"})
		return
	}
	if c.Model == "mxiang.camera.mwc10" {
		fail(w, 409, "รายการนี้เป็นตัวรับสัญญาณ ไม่ใช่กล้องที่มีมอเตอร์หมุน")
		return
	}
	if c.ParentID != "" && c.Lens == 2 {
		fail(w, 409, "ยังไม่ยืนยันการหมุนแยกเลนส์ ใช้ช่องเลนส์ 1 เพื่อควบคุมกล้อง")
		return
	}
	reply, err := a.xiaomiMotor(ctx, c, 6)
	if err != nil || !validMotorPosition(reply) {
		fail(w, 502, "ยังอ่านตำแหน่งมอเตอร์ไม่ได้ เปิดภาพสดก่อนแล้วลองตรวจอีกครั้ง รุ่นนี้อาจยังไม่รองรับคำสั่งเดียวกัน")
		return
	}
	c.Source.PTZProtocol = "miss-step"
	c.PTZ = true
	if a.store.saveCamera(c) != nil {
		fail(w, 500, "บันทึกความสามารถไม่สำเร็จ")
		return
	}
	jsonOut(w, 200, map[string]any{"supported": true, "angle": *reply.Angle, "elevation": *reply.Elevation, "message": "อ่านตำแหน่งมอเตอร์ได้และเปิดปุ่มหมุนแล้ว · ยังต้องตรวจการเคลื่อนไหวจริง"})
}
