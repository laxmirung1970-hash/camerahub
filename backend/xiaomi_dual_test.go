package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func dualFixture(t *testing.T) []Camera {
	t.Helper()
	raw := []byte(`{"sources":[{"name":"Dual fixture","url":"xiaomi://123456:sg@192.168.1.50?did=99999&model=chuangmi.camera.072ae2&token=fixture-private-token"}]}`)
	cs, err := parseXiaomiSources(raw, Connection{Account: "123456", Region: "sg"})
	if err != nil || len(cs) != 2 {
		t.Fatal("dual fixture was not expanded")
	}
	return cs
}

func TestXiaomiDualDiscoverySeparatesBothLensesAndDeduplicates(t *testing.T) {
	cs := dualFixture(t)
	if cs[0].ID != cameraID("xiaomi", "123456:99999") || cs[0].ParentID != "" || cs[1].ID == cs[0].ID || cs[1].ParentID != cs[0].ID {
		t.Fatal("primary identity changed or lenses share an identity")
	}
	for i, c := range cs {
		if c.Lens != i+1 || c.LensCount != 2 || c.Ref != "99999" || c.Model != xiaomiC300Dual {
			t.Fatal("incorrect physical device/lens metadata")
		}
		for _, quality := range []struct{ raw, subtype string }{{c.Source.Low, "sd"}, {c.Source.High, "hd"}} {
			u, err := url.Parse(quality.raw)
			if err != nil || u.Query().Get("subtype") != quality.subtype || u.Query().Get("token") != "fixture-private-token" {
				t.Fatal("lens lost quality or authentication parameters")
			}
			channel := u.Query().Get("channel")
			if (i == 0 && channel != "") || (i == 1 && channel != "2") {
				t.Fatal("two entries would play the same lens")
			}
		}
	}
	// Some gateways may list each lens or repeat a record. Still one pair.
	raw, _ := json.Marshal(map[string]any{"sources": []xiaomiSource{{Name: "Dual", URL: cs[0].Source.Low}, {Name: "Dual duplicate", URL: cs[1].Source.High}}})
	again, err := parseXiaomiSources(raw, Connection{Account: "123456", Region: "sg"})
	if err != nil || len(again) != 2 || again[0].ID != cs[0].ID || again[1].ID != cs[1].ID {
		t.Fatal("duplicate source produced additional lens entries")
	}
	unknown := Camera{ID: "single", Vendor: "xiaomi", Model: "xiaomi.camera.c01a01", Name: "Dual in an alias"}
	one, err := xiaomiLenses(unknown)
	if err != nil || len(one) != 1 || one[0].LensCount != 0 {
		t.Fatal("name incorrectly inferred a second physical lens")
	}
}

func TestXiaomiDualMigrationKeepsAliasesOrderAndIndependentResults(t *testing.T) {
	a, h := testApp(t, "live")
	owner := setupOwner(t, a, h)
	cs := dualFixture(t)
	parent := cs[0]
	parent.Lens, parent.LensCount = 0, 0 // legacy inventory, before this update
	parent.Name, parent.Group = "โรงฝึก", "ชั้นล่าง"
	parent.Favorite, parent.PTZ = true, true
	parent.Source.PTZProtocol = "miss-step"
	parent.Status, parent.Message = "online", "last real video"
	parent.LastSeen = time.Unix(1700000000, 0).UTC()
	parent.Verification, parent.AudioVerified, parent.PTZVerified = "verified", true, true
	for _, c := range []Camera{{ID: "other", Name: "Other"}, parent, {ID: "last", Name: "Last"}} {
		if err := a.store.saveCamera(c); err != nil {
			t.Fatal(err)
		}
	}
	order, _ := json.Marshal([]string{"other", parent.ID, "last"})
	if err := a.store.setSetting("camera_order", string(order)); err != nil {
		t.Fatal(err)
	}
	// Exercise the real startup migration: no cloud/network is available.
	restarted, err := newApp(a.store, a.data, "http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.stopAll()
	all, _ := a.store.cameras()
	if len(all) != 4 || all[1].ID != parent.ID || all[2].ID != cs[1].ID || all[3].ID != "last" {
		t.Fatal("new lens did not appear beside its parent")
	}
	first, second := all[1], all[2]
	if first.Name != parent.Name || first.Group != parent.Group || !first.Favorite || !first.PTZ || !first.PTZVerified || first.Verification != "verified" || !first.LastSeen.Equal(parent.LastSeen) || first.Status != "online" {
		t.Fatal("migration lost existing settings or owner verification")
	}
	if second.Group != parent.Group || second.Status != "pending" || !second.LastSeen.IsZero() || second.PTZ || second.PTZVerified || second.AudioVerified || second.Verification != "untested" {
		t.Fatal("new lens copied a verification or motor capability without testing")
	}
	second.Name, second.Group = "มุมตู้เก็บของ", "มุมพิเศษ"
	second.Status, second.Message = "error", "second lens unreachable"
	second.LastSeen = time.Unix(1700000100, 0).UTC()
	second.Favorite = true
	if err := a.store.saveCamera(second); err != nil {
		t.Fatal(err)
	}
	customOrder := []string{second.ID, "last", parent.ID, "other"}
	if r := doTest(t, h, "PUT", "/api/cameras/order", map[string]any{"ids": customOrder}, owner); r.Code != 200 {
		t.Fatal("custom order failed")
	}
	for i := 0; i < 2; i++ {
		if err := a.store.migrateXiaomiDualLenses(); err != nil {
			t.Fatal(err)
		}
	}
	all, _ = a.store.cameras()
	ids := []string{}
	for _, c := range all {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, customOrder) || all[0].Name != second.Name || all[0].Group != second.Group || all[0].Status != "error" || all[0].Message != second.Message || !all[0].Favorite || !all[0].LastSeen.Equal(second.LastSeen) {
		t.Fatal("restart/sync erased a lens alias, custom position or last result")
	}
	// A DHCP change must update both sources while retaining their identities.
	for i := range cs {
		cs[i].Host = "192.168.1.77"
		cs[i].Source.Low = strings.ReplaceAll(cs[i].Source.Low, "192.168.1.50", "192.168.1.77")
		cs[i].Source.High = strings.ReplaceAll(cs[i].Source.High, "192.168.1.50", "192.168.1.77")
	}
	if _, err := a.store.mergeDiscoveredCameras(cs); err != nil {
		t.Fatal(err)
	}
	updated, _ := a.store.camera(second.ID)
	if updated.Name != second.Name || updated.Group != second.Group || updated.Host != "192.168.1.77" || updated.Status != "pending" || !strings.Contains(updated.Source.Low, "channel=2") {
		t.Fatal("IP refresh lost a lens or its alias")
	}
	if r := doTest(t, h, "POST", "/api/cameras/"+second.ID+"/ptz/probe", map[string]string{}, owner); r.Code != 409 {
		t.Fatal("secondary lens incorrectly inferred an independent motor")
	}
	if r := doTest(t, h, "GET", "/api/cameras", nil, nil); r.Code != 401 {
		t.Fatal("anonymous inventory access")
	}
	if r := doTest(t, h, "GET", "/api/cameras/"+second.ID+"/ws", nil, nil); r.Code != 401 {
		t.Fatal("anonymous second-lens video access")
	}
	public := doTest(t, h, "GET", "/api/cameras", nil, owner).Body.Bytes()
	if bytes.Contains(public, []byte("fixture-private-token")) || bytes.Contains(public, []byte("xiaomi://")) || bytes.Contains(public, []byte(`"source"`)) {
		t.Fatal("lens API disclosed a private source")
	}
	var secret []byte
	if err := a.store.DB.QueryRow("SELECT secret FROM cameras WHERE id=?", second.ID).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(secret, []byte("fixture-private-token")) {
		t.Fatal("second-lens source stored in plaintext")
	}
}

func TestXiaomiDualMigrationRollsBackInventoryAndOrderTogether(t *testing.T) {
	a, _ := testApp(t, "live")
	parent := dualFixture(t)[0]
	parent.Lens, parent.LensCount = 0, 0
	if err := a.store.saveCamera(parent); err != nil {
		t.Fatal(err)
	}
	originalOrder := `["` + parent.ID + `"]`
	if err := a.store.setSetting("camera_order", originalOrder); err != nil {
		t.Fatal(err)
	}
	_, err := a.store.DB.Exec(`CREATE TRIGGER fail_new_lens BEFORE INSERT ON cameras WHEN NEW.id != '` + parent.ID + `' BEGIN SELECT RAISE(ABORT,'fixture write failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.store.migrateXiaomiDualLenses(); err == nil {
		t.Fatal("expected failed migration")
	}
	all, _ := a.store.cameras()
	if len(all) != 1 || all[0].Lens != 0 || a.store.setting("camera_order") != originalOrder {
		t.Fatal("failed migration left a partial inventory/order")
	}
	if _, err = a.store.DB.Exec("DROP TRIGGER fail_new_lens"); err != nil {
		t.Fatal(err)
	}
	if err = a.store.migrateXiaomiDualLenses(); err != nil {
		t.Fatal(err)
	}
	all, _ = a.store.cameras()
	if len(all) != 2 || all[0].Lens != 1 || all[1].Lens != 2 {
		t.Fatal("migration did not recover")
	}
}
