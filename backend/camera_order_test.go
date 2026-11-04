package main

import (
	"encoding/json"
	"testing"
)

func TestCameraOrderPersistsAndAppendsNewCameras(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	cameras, _ := a.store.cameras()
	first, last := cameras[0].ID, cameras[len(cameras)-1].ID
	body := map[string]any{"ids": []string{last, first}}
	if r := doTest(t, h, "PUT", "/api/cameras/order", body, nil); r.Code != 401 {
		t.Fatalf("anonymous order: %d", r.Code)
	}
	r := doTest(t, h, "PUT", "/api/cameras/order", body, owner)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var result []Camera
	json.Unmarshal(r.Body.Bytes(), &result)
	if len(result) != len(cameras) || result[0].ID != last || result[1].ID != first {
		t.Fatal("order lost cameras")
	}
	added := Camera{ID: "new-camera", Name: "new camera"}
	if err := a.store.saveCamera(added); err != nil {
		t.Fatal(err)
	}
	persisted, _ := a.store.cameras()
	if persisted[0].ID != last || persisted[len(persisted)-1].ID != added.ID {
		t.Fatal("order/new camera persistence failed")
	}
	for _, ids := range [][]string{{last, last}, {"unknown"}} {
		if r = doTest(t, h, "PUT", "/api/cameras/order", map[string]any{"ids": ids}, owner); r.Code != 400 {
			t.Fatal("accepted invalid order")
		}
	}
	persisted, _ = a.store.cameras()
	if persisted[0].ID != last {
		t.Fatal("invalid request changed order")
	}
}
