package main

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWarmHoldsOneVideoConsumerThenReleases(t *testing.T) {
	box := func(kind string, payload []byte) []byte {
		b := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:8], kind)
		copy(b[8:], payload)
		return b
	}
	fragment := append(box("moof", []byte{0, 0, 0, 1}), box("mdat", []byte{1, 2, 3, 4})...)
	var opened, closed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		opened.Add(1)
		var offer wsMessage
		if ws.ReadJSON(&offer) != nil || offer.Type != "mse" {
			t.Error("warm must ask for video like the probe")
			return
		}
		_ = ws.WriteJSON(wsMessage{Type: "mse", Value: `video/mp4; codecs="hvc1.1.6.L153.B0"`})
		_ = ws.WriteMessage(websocket.BinaryMessage, fragment)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				closed.Add(1)
				return
			}
		}
	}))
	defer server.Close()
	bridge := &Bridge{Base: server.URL}
	if !bridge.warm("cam_low", 200*time.Millisecond) || bridge.warm("cam_low", 200*time.Millisecond) {
		t.Fatal("a stream must be warmed once at a time")
	}
	deadline := time.Now().Add(3 * time.Second)
	for closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if opened.Load() != 1 || closed.Load() != 1 {
		t.Fatalf("opened=%d closed=%d", opened.Load(), closed.Load())
	}
	for time.Now().Before(deadline) {
		bridge.mu.Lock()
		n := len(bridge.warming)
		bridge.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("finished warm still blocks the stream")
}

func TestWarmLimitAndEndpoint(t *testing.T) {
	bridge := &Bridge{warming: map[string]bool{}}
	for i := 0; i < maxWarming; i++ {
		bridge.warming[strings.Repeat("x", i+1)] = true
	}
	if bridge.warm("one_more", time.Second) {
		t.Fatal("warm limit not enforced")
	}
	a, h := testApp(t, "demo")
	gateway := readyGateway(t, nil)
	a.bridge.Base = gateway.URL
	cs, _ := a.store.cameras()
	if r := doTest(t, h, "POST", "/api/cameras/"+cs[0].ID+"/warm", map[string]string{}, nil); r.Code != 401 {
		t.Fatalf("warm without session = %d", r.Code)
	}
	owner := setupOwner(t, a, h)
	if r := doTest(t, h, "POST", "/api/cameras/"+cs[0].ID+"/warm?quality=small", map[string]string{}, owner); r.Code != http.StatusAccepted {
		t.Fatalf("warm = %d %s", r.Code, r.Body.String())
	}
}
