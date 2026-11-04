package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"time"
)

// This model has two independently selectable video channels. Do not infer
// additional channels from a user-editable name or a similar model number.
const xiaomiC300Dual = "chuangmi.camera.072ae2"

func xiaomiDualCamera(c Camera) bool {
	return c.Vendor == "xiaomi" && c.Model == xiaomiC300Dual && c.ParentID == "" && c.Lens != 2
}

func xiaomiLensSource(raw string, lens int) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "xiaomi" || u.Query().Get("did") == "" || validateCameraHost(u.Hostname()) != nil {
		return "", errors.New("invalid Xiaomi lens source")
	}
	q := u.Query()
	if lens == 2 {
		q.Set("channel", "2")
	} else {
		// go2rtc 1.9.14 selects the main lens only when channel is absent.
		q.Del("channel")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func xiaomiLenses(c Camera) ([]Camera, error) {
	if !xiaomiDualCamera(c) {
		return []Camera{c}, nil
	}
	var err error
	first := c
	first.Lens, first.LensCount = 1, 2
	if first.Source.Low, err = xiaomiLensSource(c.Source.Low, 1); err != nil {
		return nil, err
	}
	if first.Source.High, err = xiaomiLensSource(c.Source.High, 1); err != nil {
		return nil, err
	}
	second := first
	// The existing primary ID stays unchanged. The second ID depends on that
	// stable ID, never an IP, name, current account token, or discovery order.
	second.ID = cameraID("xiaomi", c.ID+":lens:2")
	second.ParentID, second.Lens = c.ID, 2
	suffix := " · เลนส์ 2"
	name := []rune(c.Name)
	if max := 60 - len([]rune(suffix)); len(name) > max {
		name = name[:max]
	}
	second.Name = string(name) + suffix
	second.Status = "pending"
	second.Message = "พบช่องภาพเลนส์ 2 แล้ว ยังไม่ตรวจภาพ"
	second.LastSeen = time.Time{}
	second.Favorite = false
	second.Verification = "untested"
	second.AudioVerified, second.PTZVerified = false, false
	// A second video channel is not proof of a second controllable motor.
	second.PTZ = false
	second.Source.PTZProtocol = "none"
	if second.Source.Low, err = xiaomiLensSource(c.Source.Low, 2); err != nil {
		return nil, err
	}
	if second.Source.High, err = xiaomiLensSource(c.Source.High, 2); err != nil {
		return nil, err
	}
	return []Camera{first, second}, nil
}

// Inventory changes and initial placement of new lens channels commit together.
// Subsequent syncs retain independently edited names, positions and test results.
func (s *Store) mergeDiscoveredCameras(incoming []Camera) (int, error) {
	if len(incoming) == 0 {
		return 0, nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	existing, err := s.cameras()
	if err != nil {
		return 0, err
	}
	known := make(map[string]Camera, len(existing))
	originalIDs := make(map[string]bool, len(existing))
	order := make([]string, 0, len(existing)+len(incoming))
	for _, c := range existing {
		known[c.ID] = c
		originalIDs[c.ID] = true
		order = append(order, c.ID)
	}
	updates := make(map[string]Camera, len(incoming))
	for _, c := range incoming {
		old, exists := known[c.ID]
		if !exists {
			order = append(order, c.ID)
		}
		c = mergeCameraRecord(c, old, exists)
		known[c.ID], updates[c.ID] = c, c
	}
	newLenses := map[string]string{}
	for _, c := range incoming {
		if c.Lens != 2 || c.ParentID == "" {
			continue
		}
		if !originalIDs[c.ID] {
			if _, parentExists := known[c.ParentID]; parentExists {
				newLenses[c.ParentID] = c.ID
			}
		}
	}
	order = placeNewLenses(order, newLenses)
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, c := range updates {
		if err = s.upsertCamera(tx, c); err != nil {
			return 0, err
		}
	}
	if len(newLenses) > 0 {
		raw, _ := json.Marshal(order)
		if err = upsertSetting(tx, "camera_order", string(raw)); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(updates), nil
}

// placeNewLenses moves each newly discovered second lens directly after its
// parent and leaves every other position untouched.
func placeNewLenses(order []string, newLenses map[string]string) []string {
	if len(newLenses) == 0 {
		return order
	}
	children := map[string]bool{}
	for _, id := range newLenses {
		children[id] = true
	}
	placed := make([]string, 0, len(order))
	for _, id := range order {
		if children[id] {
			continue
		}
		placed = append(placed, id)
		if child, ok := newLenses[id]; ok {
			placed = append(placed, child)
		}
	}
	return placed
}

// Expand cameras already linked in this installation without cloud login,
// rediscovery or a new password. This also repairs a partially old inventory.
func (s *Store) migrateXiaomiDualLenses() error {
	cameras, err := s.cameras()
	if err != nil {
		return err
	}
	var expanded []Camera
	for _, c := range cameras {
		if !xiaomiDualCamera(c) {
			continue
		}
		lenses, e := xiaomiLenses(c)
		if e != nil {
			return e
		}
		expanded = append(expanded, lenses...)
	}
	_, err = s.mergeDiscoveredCameras(expanded)
	return err
}
