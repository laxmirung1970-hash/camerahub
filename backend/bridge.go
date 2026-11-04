package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Bridge struct {
	Base       string
	Client     *http.Client
	mu         sync.Mutex
	registered map[string]string
}

func (b *Bridge) request(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
	req, e := http.NewRequestWithContext(ctx, method, b.Base+path, body)
	if e != nil {
		return nil, 0, e
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, e := b.Client.Do(req)
	if e != nil {
		return nil, 0, e
	}
	defer res.Body.Close()
	data, e := limitRead(res.Body, 8<<20)
	return data, res.StatusCode, e
}
func (b *Bridge) healthy(ctx context.Context) bool {
	raw, status, e := b.request(ctx, "GET", "/api", nil)
	var info struct {
		Startup string `json:"startup"`
	}
	return e == nil && status == 200 && json.Unmarshal(raw, &info) == nil && info.Startup == "ready"
}

func (b *Bridge) dialWS(ctx context.Context, source string) (*websocket.Conn, error) {
	u, err := url.Parse(b.Base)
	if err != nil {
		return nil, errors.New("invalid gateway address")
	}
	header := http.Header{}
	if u.User != nil {
		password, _ := u.User.Password()
		header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+password)))
		u.User = nil
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/ws"
	u.RawQuery = url.Values{"src": {source}}.Encode()
	conn, res, err := websocket.DefaultDialer.DialContext(ctx, u.String(), header)
	if err != nil && res != nil && res.Body != nil {
		_ = res.Body.Close()
	}
	return conn, err
}
func (b *Bridge) ensure(ctx context.Context, c Camera, q string) error {
	src := c.Source.Low
	if q == "high" && c.Source.High != "" {
		src = c.Source.High
	}
	if src == "" {
		return errors.New("camera source not configured")
	}
	name := streamName(c, q)
	if c.Verification == "demo" {
		// Share each fixture producer. Separate FFmpeg producers with identical
		// commands collide in go2rtc's hashed RTSP return path.
		src = strings.Replace(src, "#input=file", "#input=demo_loop", 1)
		shared := "_demo_" + tokenHash(src)[:16]
		if err := b.register(ctx, shared, src); err != nil {
			return err
		}
		src = "rtsp://127.0.0.1:8554/" + shared
	}
	if c.PlaybackMode == "h264" {
		if err := b.register(ctx, name+"_native", src); err != nil {
			return err
		}
		width := "640"
		if q == "high" {
			width = "1280"
		}
		src = "ffmpeg:" + name + "_native#video=h264#audio=aac#width=" + width
	}
	return b.register(ctx, name, src)
}
func (b *Bridge) register(ctx context.Context, name, src string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.registered == nil {
		b.registered = map[string]string{}
	}
	if b.registered[name] == src {
		_, s, e := b.request(ctx, "GET", "/api/streams?src="+url.QueryEscape(name), nil)
		if e == nil && s == 200 {
			return nil
		}
	}
	query := url.Values{"name": {name}, "src": {src}}
	_, status, e := b.request(ctx, "PUT", "/api/streams?"+query.Encode(), nil)
	if e != nil {
		return e
	}
	if status >= 300 {
		return errors.New("gateway registration failed")
	}
	b.registered[name] = src
	return nil
}
func (b *Bridge) ensureRTC(ctx context.Context, c Camera, quality string) (string, error) {
	name := streamName(c, quality)
	if err := b.ensure(ctx, c, quality); err != nil {
		return "", err
	}
	rtc := name + "_rtc"
	// A separate stream prevents audio-only codec alternatives from being selected by HLS/MSE.
	return rtc, b.register(ctx, rtc, "ffmpeg:"+name+"#video=copy#audio=opus")
}
func (b *Bridge) probe(ctx context.Context, src string) error {
	// A 200 from /api/streams only proves registration. Ask for video-only
	// fragmented MP4 and require media payload, not just codec metadata.
	conn, err := b.dialWS(ctx, src)
	if err != nil {
		return errors.New("stream probe unavailable")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	conn.SetReadLimit(8 << 20)
	if err = conn.WriteJSON(map[string]string{"type": "mse", "value": "avc1.640029,avc1.64002A,avc1.640033,hvc1.1.6.L153.B0"}); err != nil {
		return errors.New("stream probe unavailable")
	}
	negotiated := false
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return errors.New("stream probe did not receive video")
		}
		if kind == websocket.TextMessage {
			var msg struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.Type == "error" {
				return errors.New("stream probe failed")
			}
			if msg.Type == "mse" {
				negotiated = strings.Contains(msg.Value, "video/mp4") && (strings.Contains(msg.Value, "avc1.") || strings.Contains(msg.Value, "hvc1."))
			}
		} else if kind == websocket.BinaryMessage && negotiated && hasVideoFragment(data) {
			return nil
		}
	}
}

func hasVideoFragment(data []byte) bool {
	moof := false
	for len(data) >= 8 {
		n := uint64(binary.BigEndian.Uint32(data))
		header := uint64(8)
		if n == 1 {
			if len(data) < 16 {
				return false
			}
			n = binary.BigEndian.Uint64(data[8:])
			header = 16
		} else if n == 0 {
			n = uint64(len(data))
		}
		if n < header || n > uint64(len(data)) {
			return false
		}
		switch string(data[4:8]) {
		case "moof":
			moof = true
		case "mdat":
			if moof && n > header {
				return true
			}
		}
		data = data[n:]
	}
	return false
}

// Diagnose the same account through RTSP without persisting a camera or URL.
// PATCH creates an in-memory stream; unlike PUT it does not update config.
func (b *Bridge) checkRTSP(ctx context.Context, source string) string {
	name := "_tapo_check_" + randomToken(12)
	query := url.Values{"name": {name}, "src": {source}}
	_, status, err := b.request(ctx, "PATCH", "/api/streams?"+query.Encode(), nil)
	if err != nil || status >= 300 {
		return "unavailable"
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _, _ = b.request(cleanup, "DELETE", "/api/streams?src="+url.QueryEscape(name), nil)
	}()
	raw, status, err := b.request(ctx, "GET", "/api/streams?src="+url.QueryEscape(name)+"&video", nil)
	if err != nil {
		return "unavailable"
	}
	if status == http.StatusOK {
		return "accepted"
	}
	message := strings.ToLower(string(raw))
	if strings.Contains(message, "401") || strings.Contains(message, "unauthorized") || strings.Contains(message, "403") || strings.Contains(message, "wrong user/pass") {
		return "rejected"
	}
	return "unavailable"
}

func (a *App) mediaCamera(w http.ResponseWriter, r *http.Request) (Camera, bool) {
	c, e := a.store.camera(r.PathValue("id"))
	if e != nil {
		fail(w, 404, "ไม่พบกล้อง")
		return c, false
	}
	if c.Status == "offline" || c.Status == "auth_expired" {
		fail(w, 409, c.Message)
		return c, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if e = a.bridge.ensure(ctx, c, qualityOf(r)); e != nil {
		fail(w, 502, "ตัวกลางวิดีโอยังไม่พร้อม กรุณาตรวจสถานะระบบ")
		return c, false
	}
	return c, true
}
func (a *App) mediaRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/cameras/{id}/webrtc", a.require(false, func(w http.ResponseWriter, r *http.Request) {
		if env("APP_WEBRTC", "false") != "true" {
			fail(w, 409, "WebRTC ยังไม่เปิดใช้ ใช้ MSE หรือ HLS ผ่าน HTTPS")
			return
		}
		c, ok := a.mediaCamera(w, r)
		if !ok {
			return
		}
		raw, e := limitRead(r.Body, 256*1024)
		if e != nil {
			fail(w, 400, "ข้อมูลวิดีโอไม่ถูกต้อง")
			return
		}
		rtc, e := a.bridge.ensureRTC(r.Context(), c, qualityOf(r))
		if e != nil {
			fail(w, 502, "เตรียม WebRTC ไม่สำเร็จ")
			return
		}
		req, e := http.NewRequestWithContext(r.Context(), "POST", a.bridge.Base+"/api/webrtc?src="+url.QueryEscape(rtc), strings.NewReader(string(raw)))
		if e != nil {
			fail(w, 502, "เปิดวิดีโอไม่สำเร็จ")
			return
		}
		req.Header.Set("Content-Type", "application/sdp")
		res, e := a.bridge.Client.Do(req)
		if e != nil {
			fail(w, 502, "เชื่อมวิดีโอไม่ได้")
			return
		}
		defer res.Body.Close()
		body, e := limitRead(res.Body, 256*1024)
		if e != nil || res.StatusCode >= 300 {
			fail(w, 502, "WebRTC ไม่พร้อม ลองวิธีสำรอง")
			return
		}
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = w.Write(body)
	}))
	m.HandleFunc("GET /api/cameras/{id}/ws", a.require(false, func(w http.ResponseWriter, r *http.Request) {
		c, ok := a.mediaCamera(w, r)
		if !ok {
			return
		}
		upstream, e := a.bridge.dialWS(r.Context(), streamName(c, qualityOf(r)))
		if e != nil {
			fail(w, 502, "เปิดวิดีโอไม่สำเร็จ")
			return
		}
		defer upstream.Close()
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
			o := r.Header.Get("Origin")
			if o == "" {
				return true
			}
			u, e := url.Parse(o)
			return e == nil && u.Host == r.Host
		}}
		client, e := upgrader.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer client.Close()
		client.SetReadLimit(65536)
		done := make(chan struct{}, 2)
		// Only negotiated media messages are accepted, never arbitrary gateway commands.
		go func() {
			defer func() { done <- struct{}{} }()
			for {
				typ, data, e := client.ReadMessage()
				if e != nil {
					return
				}
				if typ != websocket.TextMessage {
					return
				}
				var msg struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				}
				if json.Unmarshal(data, &msg) != nil || msg.Type != "mse" {
					return
				}
				if upstream.WriteMessage(typ, data) != nil {
					return
				}
			}
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			for {
				typ, data, e := upstream.ReadMessage()
				if e != nil {
					return
				}
				if typ == websocket.TextMessage {
					var message struct {
						Type  string `json:"type"`
						Value string `json:"value"`
					}
					if json.Unmarshal(data, &message) != nil {
						return
					}
					if message.Type == "error" {
						data, _ = json.Marshal(map[string]string{"type": "error", "value": mediaErrorCategory(message.Value)})
					} else if message.Type != "mse" {
						continue
					}
				}
				if client.WriteMessage(typ, data) != nil {
					return
				}
			}
		}()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		watching := true
		for watching {
			select {
			case <-done:
				watching = false
			case <-r.Context().Done():
				watching = false
			case <-ticker.C:
				if _, ok := a.principal(r); !ok {
					watching = false
				}
			}
		}
		a.stopCamera(c.ID, principalID(a, r))
	}))
	m.HandleFunc("GET /api/cameras/{id}/hls", a.require(false, func(w http.ResponseWriter, r *http.Request) {
		c, ok := a.mediaCamera(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		body, status, e := a.bridge.request(ctx, "GET", "/api/stream.m3u8?src="+url.QueryEscape(streamName(c, qualityOf(r)))+"&mp4=all", nil)
		if e != nil || status != 200 || !strings.Contains(string(body), "#EXTM3U") {
			fail(w, 502, "สร้างวิดีโอ HLS ไม่สำเร็จ")
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte(rewritePlaylist(string(body))))
	}))
	m.HandleFunc("GET /api/media/hls/{file}", a.require(false, func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		if file != "playlist.m3u8" && file != "init.mp4" && file != "segment.m4s" && file != "segment.ts" {
			http.NotFound(w, r)
			return
		}
		q := url.Values{}
		for _, k := range []string{"id", "n"} {
			if v := r.URL.Query().Get(k); v != "" && len(v) < 120 {
				q.Set(k, v)
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		body, status, e := a.bridge.request(ctx, "GET", "/api/hls/"+file+"?"+q.Encode(), nil)
		if e != nil || status != 200 {
			http.Error(w, "media unavailable", 502)
			return
		}
		if file == "playlist.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			body = []byte(rewritePlaylist(string(body)))
		} else if file == "init.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
		} else if file == "segment.ts" {
			w.Header().Set("Content-Type", "video/mp2t")
		} else {
			w.Header().Set("Content-Type", "video/iso.segment")
		}
		_, _ = w.Write(body)
	}))
}
func rewritePlaylist(body string) string {
	body = strings.ReplaceAll(body, "hls/playlist.m3u8?", "/api/media/hls/playlist.m3u8?")
	for _, file := range []string{"init.mp4", "segment.m4s", "segment.ts", "playlist.m3u8"} {
		body = strings.ReplaceAll(body, "URI=\""+file+"?", "URI=\"/api/media/hls/"+file+"?")
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, file+"?") {
				lines[i] = "/api/media/hls/" + line
			}
		}
		body = strings.Join(lines, "\n")
	}
	return body
}
func principalID(a *App, r *http.Request) string { p, _ := a.principal(r); return p.ID }
