package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func encodeBackup(t *testing.T, password string, in backupData) string {
	t.Helper()
	raw, _ := json.Marshal(in)
	enc, err := backupEncrypt(password, raw)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(enc)
}

func TestBackupValidationRejectsForeignSources(t *testing.T) {
	tapo := Camera{ID: "t", Vendor: "tapo", Host: "172.16.0.92"}
	xiaomi := Camera{ID: "x", Vendor: "xiaomi", Host: "172.16.0.50"}
	good := func(c Camera, s Source) backupData {
		return backupData{Version: 1, Mode: "live", Cameras: []backupCamera{{c, s}}}
	}
	if err := validateBackup(good(tapo, Source{Low: "rtsp://u:p@172.16.0.92:554/stream2", High: "rtsp://u:p@172.16.0.92:554/stream1", DeviceURL: "http://172.16.0.92:2020/onvif/device_service", PTZProtocol: "onvif"}), "live"); err != nil {
		t.Fatal("valid Tapo backup rejected: ", err)
	}
	if err := validateBackup(good(xiaomi, Source{Low: "xiaomi://1:sg@172.16.0.50?did=1&subtype=sd", PTZProtocol: "miss-step"}), "live"); err != nil {
		t.Fatal("valid Xiaomi backup rejected: ", err)
	}
	for name, in := range map[string]backupData{
		"exec":           good(tapo, Source{Low: "exec:sh -c id"}),
		"public_host":    good(Camera{ID: "p", Vendor: "tapo", Host: "8.8.8.8"}, Source{Low: "rtsp://8.8.8.8/stream1"}),
		"other_host":     good(tapo, Source{Low: "rtsp://172.16.0.93/stream1"}),
		"metadata_url":   good(tapo, Source{Low: "rtsp://172.16.0.92/stream1", DeviceURL: "http://169.254.169.254/latest"}),
		"xiaomi_public":  good(xiaomi, Source{Low: "xiaomi://1:sg@203.0.113.5?did=1"}),
		"unknown_vendor": good(Camera{ID: "u", Vendor: "other", Host: "172.16.0.9"}, Source{Low: "rtsp://172.16.0.9/x"}),
		"duplicate_id":   {Version: 1, Mode: "live", Cameras: []backupCamera{{tapo, Source{Low: "rtsp://172.16.0.92/s"}}, {tapo, Source{Low: "rtsp://172.16.0.92/s"}}}},
		"wrong_mode":     {Version: 1, Mode: "demo"},
		"bad_remote":     {Version: 1, Mode: "live", RemoteURL: "http://plain.example"},
		"bad_connection": {Version: 1, Mode: "live", Connections: []backupConnection{{Connection: Connection{ID: "c", Vendor: "xiaomi", Account: "abc", Region: "sg"}}}},
		"demo_exec":      {Version: 1, Mode: "demo", Cameras: []backupCamera{{Camera{ID: "d"}, Source{Low: "ffmpeg:/media/x.mp4#input=file|exec:id"}}}},
	} {
		mode := in.Mode
		if name == "wrong_mode" {
			mode = "live"
		}
		if validateBackup(in, mode) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestRestoreCommitsDatabaseBeforeGatewayFile(t *testing.T) {
	dir := t.TempDir()
	gatewayFile := filepath.Join(dir, "go2rtc.yaml")
	_ = os.WriteFile(gatewayFile, []byte("fixture: 1\n"), 0600)
	t.Setenv("GATEWAY_CONFIG", gatewayFile)
	a, h := testApp(t, "live")
	owner := setupOwner(t, a, h)
	original := Camera{ID: "keep", Vendor: "tapo", Host: "172.16.0.92", Name: "Original", Source: Source{Low: "rtsp://u:fixture-private@172.16.0.92/stream2", High: "rtsp://u:fixture-private@172.16.0.92/stream1"}}
	_ = a.store.saveCamera(original)
	_ = a.store.saveConnection(Connection{ID: "conn", Vendor: "tapo", Username: "u", Password: "fixture-private"})
	export := doTest(t, h, "POST", "/api/backup", map[string]string{"password": "fixture-backup-password"}, owner)
	if export.Code != 200 {
		t.Fatal(export.Body.String())
	}
	password := "fixture-backup-password"
	bad := encodeBackup(t, password, backupData{Version: 1, Mode: "live", Cameras: []backupCamera{{Camera{ID: "evil", Vendor: "tapo", Host: "172.16.0.92"}, Source{Low: "exec:sh -c id"}}}})
	if r := doTest(t, h, "POST", "/api/backup/restore", map[string]string{"password": password, "file": bad, "confirm": "RESTORE"}, owner); r.Code != 400 {
		t.Fatalf("crafted source accepted: %d", r.Code)
	}
	if cs, _ := a.store.cameras(); len(cs) != 1 || cs[0].ID != "keep" {
		t.Fatal("rejected restore changed the registry")
	}
	replacement := encodeBackup(t, password, backupData{Version: 1, Mode: "live", Gateway: []byte("fixture: 2\n"), Cameras: []backupCamera{{Camera{ID: "restored", Vendor: "xiaomi", Host: "172.16.0.50"}, Source{Low: "xiaomi://1:sg@172.16.0.50?did=1"}}}})
	_ = os.RemoveAll(dir)
	r := doTest(t, h, "POST", "/api/backup/restore", map[string]string{"password": password, "file": replacement, "confirm": "RESTORE"}, owner)
	if r.Code != 500 {
		t.Fatalf("missing gateway directory not reported: %d", r.Code)
	}
	if _, err := a.store.camera("restored"); err != nil {
		t.Fatal("database restore was rolled back although it had committed")
	}
	if _, err := a.store.camera("keep"); err == nil {
		t.Fatal("old registry survived restore")
	}
	_ = os.MkdirAll(dir, 0700)
	r = doTest(t, h, "POST", "/api/backup/restore", map[string]string{"password": password, "file": base64.StdEncoding.EncodeToString(export.Body.Bytes()), "confirm": "RESTORE"}, owner)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	cs, _ := a.store.cameras()
	conns, _ := a.store.connections()
	raw, _ := os.ReadFile(gatewayFile)
	if len(cs) != 1 || cs[0].Name != "Original" || cs[0].Source.Low != original.Source.Low || len(conns) != 1 || conns[0].Password != "fixture-private" || string(raw) != "fixture: 1\n" {
		t.Fatal("round trip lost cameras, credentials or gateway config")
	}
}
