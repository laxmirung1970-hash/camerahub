package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHLSFileWaitsForGatewaySegment(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	var calls atomic.Int32
	gateway := readyGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hls/init.mp4" {
			w.WriteHeader(200)
			return
		}
		if calls.Add(1) < 3 {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "init")
	})
	a.bridge.Base = gateway.URL
	r := doTest(t, h, "GET", "/api/media/hls/init.mp4?id=fixture", nil, owner)
	if r.Code != 200 || r.Body.String() != "init" || calls.Load() != 3 {
		t.Fatalf("init = %d %q after %d calls", r.Code, r.Body.String(), calls.Load())
	}
	old := hlsWait
	hlsWait = 300 * time.Millisecond
	t.Cleanup(func() { hlsWait = old })
	calls.Store(-100)
	if r := doTest(t, h, "GET", "/api/media/hls/init.mp4?id=fixture", nil, owner); r.Code != 502 {
		t.Fatalf("missing init after wait = %d", r.Code)
	}
}

func TestMP4StreamsUntilClientLeavesAndMapsMissingRoute(t *testing.T) {
	a, h := testApp(t, "demo")
	owner := setupOwner(t, a, h)
	var enabled atomic.Bool
	released := make(chan struct{})
	gateway := readyGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stream.mp4" {
			w.WriteHeader(200)
			return
		}
		if !enabled.Load() {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("mp4") != "all" || !strings.HasSuffix(r.URL.Query().Get("src"), "_low") {
			t.Errorf("unexpected gateway query %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", `video/mp4; codecs="avc1.640029"`)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "chunk\n")
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
		close(released)
	})
	a.bridge.Base = gateway.URL
	cs, _ := a.store.cameras()
	path := "/api/cameras/" + cs[0].ID + "/mp4"
	if r := doTest(t, h, "GET", path, nil, nil); r.Code != 401 {
		t.Fatalf("mp4 without session = %d", r.Code)
	}
	if r := doTest(t, h, "GET", path, nil, owner); r.Code != 404 {
		t.Fatalf("gateway without mp4 route = %d, want 404 for HLS fallback", r.Code)
	}
	enabled.Store(true)
	server := httptest.NewServer(h)
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.AddCookie(owner)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "video/mp4") {
		t.Fatalf("mp4 = %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	lines := bufio.NewScanner(res.Body)
	for i := 0; i < 3; i++ {
		if !lines.Scan() || lines.Text() != "chunk" {
			t.Fatal("stream bytes were not flushed through")
		}
	}
	_ = res.Body.Close()
	select {
	case <-released:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway consumer kept open after the client left")
	}
}
