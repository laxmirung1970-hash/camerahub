package main

import (
	"encoding/json"
	"net/http"
	"sort"
)

func applyCameraOrder(cameras []Camera, raw string) {
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return
	}
	positions := make(map[string]int, len(ids))
	for i, id := range ids {
		positions[id] = i
	}
	sort.SliceStable(cameras, func(i, j int) bool {
		a, foundA := positions[cameras[i].ID]
		b, foundB := positions[cameras[j].ID]
		if foundA != foundB {
			return foundA
		}
		return foundA && a < b
	})
}

func (a *App) cameraOrderHandler(w http.ResponseWriter, r *http.Request) {
	// Discovery can insert new lens channels beside a camera at the same time.
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	var body struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	cameras, err := a.store.cameras()
	if err != nil {
		fail(w, 500, "อ่านรายการกล้องไม่สำเร็จ")
		return
	}
	known := make(map[string]bool, len(cameras))
	seen := map[string]bool{}
	for _, c := range cameras {
		known[c.ID] = true
	}
	for _, id := range body.IDs {
		if !known[id] || seen[id] {
			fail(w, 400, "รายการลำดับกล้องไม่ถูกต้องหรือซ้ำกัน")
			return
		}
		seen[id] = true
	}
	// Cameras discovered after the screen loaded retain a place after this order.
	for _, c := range cameras {
		if !seen[c.ID] {
			body.IDs = append(body.IDs, c.ID)
		}
	}
	raw, _ := json.Marshal(body.IDs)
	if a.store.setSetting("camera_order", string(raw)) != nil {
		fail(w, 500, "บันทึกลำดับไม่สำเร็จ")
		return
	}
	applyCameraOrder(cameras, string(raw))
	jsonOut(w, 200, cameras)
}
