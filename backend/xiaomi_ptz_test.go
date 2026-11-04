package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestXiaomiMotorProbeRequiresPositionAndOwner(t *testing.T) {
	a, h := testApp(t, "live")
	owner := setupOwner(t, a, h)
	reply := `{"angle":49,"elevation":51,"ret":0}` + "\x00"
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/xiaomi/command" || r.URL.Query().Get("data") != `{"operation":6}` {
			t.Error("unexpected command")
		}
		json.NewEncoder(w).Encode(map[string]string{"reply": reply})
	}))
	defer gateway.Close()
	a.bridge = &Bridge{Base: gateway.URL, Client: gateway.Client()}
	camera := Camera{ID: "motor", Vendor: "xiaomi", Model: "xiaomi.camera.c01a01", Source: Source{PTZProtocol: "none"}}
	a.store.saveCamera(camera)
	if r := doTest(t, h, "POST", "/api/cameras/motor/ptz/probe", map[string]string{}, nil); r.Code != 401 {
		t.Fatal("anonymous probe")
	}
	if r := doTest(t, h, "POST", "/api/cameras/motor/ptz/probe", map[string]string{}, owner); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	saved, _ := a.store.camera("motor")
	if !saved.PTZ || saved.Source.PTZProtocol != "miss-step" || saved.PTZVerified {
		t.Fatal("incorrect capability/verification")
	}
	for _, bad := range []string{`{"angle":49,"elevation":51,"ret":5}`, `{"ret":-5}`, `not-json`} {
		reply = bad
		if _, err := a.xiaomiMotor(context.Background(), camera, 6); err == nil {
			t.Fatal("accepted malformed/rejected response")
		}
	}
	reply = `{"angle":49,"elevation":51,"ret":-5}`
	if _, err := a.xiaomiMotor(context.Background(), camera, 6); err != nil {
		t.Fatal("valid terminal position rejected")
	}
	reply = `{"angle":57.48,"elevation":9.75,"ret":0}`
	position, err := a.xiaomiMotor(context.Background(), camera, 6)
	if err != nil || !validMotorPosition(position) || *position.Angle != 57.48 || *position.Elevation != 9.75 {
		t.Fatal("fractional C302 position rejected or truncated")
	}
}
func TestMediaErrorCategoriesNeverContainSecrets(t *testing.T) {
	for _, sample := range []struct{ raw, want string }{{"rtsp://name:private@192.0.2.9 timeout", "timeout"}, {"401 unauthorized token=private", "auth"}, {"unsupported codec /private/path", "unsupported_codec"}, {"secret unknown", "camera_unavailable"}} {
		if got := mediaErrorCategory(sample.raw); got != sample.want {
			t.Fatalf("wrong fixed category: %q", got)
		}
	}
}
