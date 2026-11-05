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
	"sync/atomic"
	"time"
)

type Bridge struct {
	Base       string
	Client     *http.Client
	mu         sync.Mutex
	registered map[string]string
	// Per-stream locks keep gateway I/O outside mu so one slow registration
	// does not stall every other camera.
	inflight map[string]*sync.Mutex
	warming  map[string]bool
}

// A warm consumer holds a stream this long after video starts, so a client
// switching to it next joins a camera that is already connected.
const warmHold = 30 * time.Second
const maxWarming = 8

// hlsWait bounds how long one HLS file request waits for go2rtc to have it.
var hlsWait = 8 * time.Second

// mp4Idle ends an MP4 stream that stopped carrying data, so a dead camera
// does not hold a player and a gateway consumer open.
var mp4Idle = 20 * time.Second

// Only these two message shapes are relayed between browser and gateway.
type wsMessage struct {
	Type  string `json:"type"`
	Value string `json:"value"`
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
	if q == "small" {
		if err := b.ensure(ctx, c, "low"); err != nil {
			return err
		}
		if name := streamName(c, q); name != streamName(c, "low") {
			return b.register(ctx, name, "ffmpeg:"+streamName(c, "low")+"#video=h264#audio=aac#width=640")
		}
		return nil
	}
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
func (b *Bridge) streamLock(name string) *sync.Mutex {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inflight == nil {
		b.inflight = map[string]*sync.Mutex{}
	}
	if b.inflight[name] == nil {
		b.inflight[name] = &sync.Mutex{}
	}
	return b.inflight[name]
}
func (b *Bridge) cached(name, src string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.registered[name] == src
}
func (b *Bridge) register(ctx context.Context, name, src string) error {
	lock := b.streamLock(name)
	lock.Lock()
	defer lock.Unlock()
	if b.cached(name, src) {
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
	b.mu.Lock()
	if b.registered == nil {
		b.registered = map[string]string{}
	}
	b.registered[name] = src
	b.mu.Unlock()
	return nil
}

// resetCache forces re-registration after the gateway config was replaced.
func (b *Bridge) resetCache() {
	b.mu.Lock()
	b.registered = nil
	b.mu.Unlock()
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
	return awaitVideo(conn, deadline)
}

// warm starts the camera behind a registered stream in the background and
// keeps one video consumer on it for hold. Xiaomi P2P can take 10–20 s to
// connect, longer than a TV tour leaves a page on screen.
func (b *Bridge) warm(src string, hold time.Duration) bool {
	b.mu.Lock()
	if b.warming == nil {
		b.warming = map[string]bool{}
	}
	if b.warming[src] || len(b.warming) >= maxWarming {
		b.mu.Unlock()
		return false
	}
	b.warming[src] = true
	b.mu.Unlock()
	go func() {
		defer func() {
			b.mu.Lock()
			delete(b.warming, src)
			b.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		conn, err := b.dialWS(ctx, src)
		cancel()
		if err != nil {
			return
		}
		defer conn.Close()
		if awaitVideo(conn, time.Now().Add(25*time.Second)) != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(hold))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	return true
}

func awaitVideo(conn *websocket.Conn, deadline time.Time) error {
	// A 200 from /api/streams only proves registration. Ask for video-only
	// fragmented MP4 and require media payload, not just codec metadata.
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	conn.SetReadLimit(8 << 20)
	if err := conn.WriteJSON(wsMessage{Type: "mse", Value: "avc1.640029,avc1.64002A,avc1.640033,hvc1.1.6.L153.B0"}); err != nil {
		return errors.New("stream probe unavailable")
	}
	negotiated := false
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return errors.New("stream probe did not receive video")
		}
		if kind == websocket.TextMessage {
			var msg wsMessage
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
	c, found := a.cameraFromPath(w, r)
	if !found {
		return c, false
	}
	if cameraUnavailable(c) {
		fail(w, 409, c.Message)
		return c, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if e := a.bridge.ensure(ctx, c, qualityOf(r)); e != nil {
		fail(w, 502, "ตัวกลางวิดีโอยังไม่พร้อม กรุณาตรวจสถานะระบบ")
		return c, false
	}
	return c, true
}
func (a *App) mediaRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/cameras/{id}/webrtc", a.require(false, a.webrtcHandler))
	m.HandleFunc("GET /api/cameras/{id}/ws", a.require(false, a.wsHandler))
	m.HandleFunc("GET /api/cameras/{id}/hls", a.require(false, a.hlsPlaylistHandler))
	m.HandleFunc("GET /api/cameras/{id}/mp4", a.require(false, a.mp4Handler))
	m.HandleFunc("POST /api/cameras/{id}/warm", a.require(false, a.warmHandler))
	m.HandleFunc("GET /api/media/hls/{file}", a.require(false, a.hlsFileHandler))
}
func (a *App) webrtcHandler(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.webRTC {
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
}
func (a *App) wsOriginOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, e := url.Parse(o)
	return e == nil && (u.Host == r.Host || a.crossOriginAllowed(r))
}
func (a *App) wsHandler(w http.ResponseWriter, r *http.Request) {
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
	// Echo "bearer" so a browser that authenticated via subprotocol accepts the handshake.
	upgrader := websocket.Upgrader{Subprotocols: []string{"bearer"}, CheckOrigin: a.wsOriginOK}
	client, e := upgrader.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer client.Close()
	client.SetReadLimit(65536)
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		pumpToGateway(client, upstream)
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		pumpToClient(upstream, client)
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
	a.stopCamera(c.ID, principalOf(r).ID)
}

// Only negotiated media messages are accepted, never arbitrary gateway commands.
func pumpToGateway(client, upstream *websocket.Conn) {
	for {
		typ, data, e := client.ReadMessage()
		if e != nil || typ != websocket.TextMessage {
			return
		}
		var msg wsMessage
		if json.Unmarshal(data, &msg) != nil || msg.Type != "mse" {
			return
		}
		if upstream.WriteMessage(typ, data) != nil {
			return
		}
	}
}
func pumpToClient(upstream, client *websocket.Conn) {
	for {
		typ, data, e := upstream.ReadMessage()
		if e != nil {
			return
		}
		if typ == websocket.TextMessage {
			var message wsMessage
			if json.Unmarshal(data, &message) != nil {
				return
			}
			if message.Type == "error" {
				data, _ = json.Marshal(wsMessage{Type: "error", Value: mediaErrorCategory(message.Value)})
			} else if message.Type != "mse" {
				continue
			}
		}
		if client.WriteMessage(typ, data) != nil {
			return
		}
	}
}

// warmHandler lets a touring client start the next page's cameras ahead of
// time; it returns at once and the gateway connection is held briefly.
func (a *App) warmHandler(w http.ResponseWriter, r *http.Request) {
	c, ok := a.mediaCamera(w, r)
	if !ok {
		return
	}
	a.bridge.warm(streamName(c, qualityOf(r)), warmHold)
	w.WriteHeader(http.StatusAccepted)
}

// mp4Handler relays go2rtc's continuous fragmented MP4. One long response
// lets the player keep its own buffer, where go2rtc HLS holds about a second.
func (a *App) mp4Handler(w http.ResponseWriter, r *http.Request) {
	c, ok := a.mediaCamera(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", a.bridge.Base+"/api/stream.mp4?src="+url.QueryEscape(streamName(c, qualityOf(r)))+"&mp4=all", nil)
	if e != nil {
		fail(w, 502, "เปิดวิดีโอไม่สำเร็จ")
		return
	}
	// The shared client's timeout would cut a live stream; only the wait for
	// the camera's first bytes is bounded.
	client := &http.Client{}
	if a.bridge.Client != nil {
		client.Transport = a.bridge.Client.Transport
	}
	startup := time.AfterFunc(25*time.Second, cancel)
	res, e := client.Do(req)
	startup.Stop()
	if e != nil {
		fail(w, 502, "เปิดวิดีโอไม่สำเร็จ")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, _ := limitRead(res.Body, 4096)
		// A gateway without /api/stream.mp4 in allow_paths answers a plain 404;
		// clients fall back to HLS on 404.
		if res.StatusCode == http.StatusNotFound && !strings.Contains(string(body), "stream") {
			fail(w, 404, "ตัวกลางวิดีโอยังไม่เปิด MP4")
			return
		}
		fail(w, 502, "เปิดวิดีโอไม่สำเร็จ")
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	contentType := res.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "video/mp4") {
		contentType = "video/mp4"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for checks := 1; ; checks++ {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if time.Since(time.Unix(0, last.Load())) > mp4Idle {
				cancel()
				return
			}
			if checks%3 == 0 {
				if _, ok := a.principal(r); !ok {
					cancel()
					return
				}
			}
		}
	}()
	buf := make([]byte, 64<<10)
	for {
		n, e := res.Body.Read(buf)
		if n > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, we := w.Write(buf[:n]); we != nil || rc.Flush() != nil {
				break
			}
			last.Store(time.Now().UnixNano())
		}
		if e != nil {
			break
		}
	}
	a.stopCamera(c.ID, principalOf(r).ID)
}
func (a *App) hlsPlaylistHandler(w http.ResponseWriter, r *http.Request) {
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
}
func (a *App) hlsFileHandler(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	contentType := map[string]string{"playlist.m3u8": "application/vnd.apple.mpegurl", "init.mp4": "video/mp4", "segment.m4s": "video/iso.segment", "segment.ts": "video/mp2t"}[file]
	if contentType == "" {
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
	// go2rtc answers 404 while a fresh session has no init or segment yet.
	// Waiting here keeps players from treating a camera that is still
	// connecting as failed and backing off for tens of seconds.
	retryUntil := time.Now().Add(hlsWait)
	body, status, e := a.bridge.request(ctx, "GET", "/api/hls/"+file+"?"+q.Encode(), nil)
	for e == nil && status == http.StatusNotFound && file != "playlist.m3u8" && time.Now().Before(retryUntil) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
		body, status, e = a.bridge.request(ctx, "GET", "/api/hls/"+file+"?"+q.Encode(), nil)
	}
	if e != nil || status != 200 {
		http.Error(w, "media unavailable", 502)
		return
	}
	if file == "playlist.m3u8" {
		body = []byte(rewritePlaylist(string(body)))
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(body)
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
