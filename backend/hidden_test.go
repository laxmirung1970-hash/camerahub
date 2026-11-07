package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestOwnerHidesCameraAndDiscoveryKeepsItHidden(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	all, _ := a.store.cameras()
	target := all[0]
	list := func(path string) []Camera {
		t.Helper()
		r := doTest(t, h, "GET", path, nil, owner)
		var cs []Camera
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &cs) != nil {
			t.Fatalf("%s = %d", path, r.Code)
		}
		return cs
	}
	if r := doTest(t, h, "PATCH", "/api/cameras/"+target.ID, map[string]any{"hidden": true}, nil); r.Code != 401 {
		t.Fatalf("hide without session = %d", r.Code)
	}
	if r := doTest(t, h, "PATCH", "/api/cameras/"+target.ID, map[string]any{"hidden": true}, owner); r.Code != 200 {
		t.Fatalf("hide = %d", r.Code)
	}
	visible := list("/api/cameras")
	if len(visible) != len(all)-1 {
		t.Fatalf("visible = %d, want %d", len(visible), len(all)-1)
	}
	for _, c := range visible {
		if c.ID == target.ID {
			t.Fatal("hidden camera still listed")
		}
	}
	if hidden := list("/api/cameras?hidden=1"); len(hidden) != 1 || hidden[0].ID != target.ID {
		t.Fatalf("hidden list = %+v", hidden)
	}

	// A discovery refresh of the same device must not bring it back.
	fresh := target
	fresh.Hidden = false
	fresh.Status = "pending"
	if err := a.store.mergeCamera(fresh); err != nil {
		t.Fatal(err)
	}
	if len(list("/api/cameras")) != len(all)-1 {
		t.Fatal("discovery un-hid the camera")
	}

	// TV and share tokens never see hidden cameras, even when asking for them.
	tv := doTestWith(t, h, "GET", "/api/cameras?hidden=1", nil, nil, tvToken(t, a, "tv"))
	var tvList []Camera
	_ = json.Unmarshal(tv.Body.Bytes(), &tvList)
	for _, c := range tvList {
		if c.ID == target.ID {
			t.Fatal("TV sees hidden camera")
		}
	}
	if len(tvList) != len(all)-1 {
		t.Fatalf("TV list = %d", len(tvList))
	}

	if r := doTest(t, h, "PATCH", "/api/cameras/"+target.ID, map[string]any{"hidden": false}, owner); r.Code != 200 {
		t.Fatalf("restore = %d", r.Code)
	}
	if len(list("/api/cameras")) != len(all) || len(list("/api/cameras?hidden=1")) != 0 {
		t.Fatal("restore did not return the camera")
	}
}

func TestXiaomiWithoutMicrophoneRegistersVideoOnly(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	var registered []string
	gateway := readyGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && r.URL.Path == "/api/streams" {
			registered = append(registered, r.URL.Query().Get("src"))
		}
		w.WriteHeader(200)
	})
	a.bridge.Base = gateway.URL
	c := Camera{ID: "xiaomi-mic-fixture", Vendor: "xiaomi", Ref: "99999", Name: "No mic", Host: "172.16.0.50", Status: "online", Audio: true, Verification: "stream_only",
		Source: Source{Low: "xiaomi://123456:sg@172.16.0.50?did=99999&model=chuangmi.camera.ipc009&subtype=sd"}}
	if err := a.store.saveCamera(c); err != nil {
		t.Fatal(err)
	}
	if err := a.bridge.ensure(context.Background(), c, "low"); err != nil || len(registered) != 1 || strings.Contains(registered[0], "audio=0") {
		t.Fatalf("with microphone registered %v (%v)", registered, err)
	}
	if r := doTest(t, h, "PATCH", "/api/cameras/"+c.ID, map[string]any{"audio": false}, owner); r.Code != 200 {
		t.Fatalf("audio off = %d", r.Code)
	}
	// A cloud refresh reports audio again; the owner's choice stays.
	if err := a.store.mergeCamera(c); err != nil {
		t.Fatal(err)
	}
	saved, _ := a.store.camera(c.ID)
	if saved.Audio {
		t.Fatal("discovery turned audio back on")
	}
	if err := a.bridge.ensure(context.Background(), saved, "low"); err != nil || len(registered) != 2 || !strings.Contains(registered[1], "audio=0") || !strings.Contains(registered[1], "did=99999") {
		t.Fatalf("video-only registration = %v (%v)", registered, err)
	}
}
