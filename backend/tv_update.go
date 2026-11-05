package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TV self-update: the hub hands its paired TVs the newest signed APK so the
// owner never needs a USB drive or adb again. The folder holds exactly one
// release, written by scripts/Publish-TVUpdate.ps1 and bundled into the image:
//
//	camerahub-tv.apk   the signed release APK
//	camerahub-tv.json  {"versionCode": 7, "versionName": "1.3.1"}
//
// The sha256 is computed here from the file, never trusted from the JSON.
// Android itself refuses an APK with another package name or signing key.
const (
	tvUpdateAPK  = "camerahub-tv.apk"
	tvUpdateMeta = "camerahub-tv.json"
	tvUpdateMax  = 200 << 20
)

var tvVersionName = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,32}$`)

type tvUpdate struct {
	VersionCode int64  `json:"versionCode"`
	VersionName string `json:"versionName"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	path        string
	modified    time.Time
}

// Hashing a 4 MB APK on the NAS is cheap but not free; keep the result until
// the file changes.
var tvUpdateCache struct {
	sync.Mutex
	item tvUpdate
}

func underDir(dir, root string) bool {
	if root == "" {
		return false
	}
	d, e1 := filepath.Abs(dir)
	r, e2 := filepath.Abs(root)
	if e1 != nil || e2 != nil {
		return true
	}
	rel, err := filepath.Rel(r, d)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// currentTVUpdate returns the release in the configured folder, or an error
// when there is none or it is unusable. The data folder holds keys and the
// database, so it is never served from even if misconfigured.
func (a *App) currentTVUpdate() (tvUpdate, error) {
	dir := a.cfg.tvUpdateDir
	if dir == "" || underDir(dir, a.data) {
		return tvUpdate{}, errors.New("disabled")
	}
	raw, err := os.ReadFile(filepath.Join(dir, tvUpdateMeta))
	if err != nil {
		return tvUpdate{}, err
	}
	var meta tvUpdate
	if json.Unmarshal(raw, &meta) != nil || meta.VersionCode <= 0 || !tvVersionName.MatchString(meta.VersionName) {
		return tvUpdate{}, errors.New("bad metadata")
	}
	path := filepath.Join(dir, tvUpdateAPK)
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > tvUpdateMax {
		return tvUpdate{}, errors.New("no apk")
	}
	tvUpdateCache.Lock()
	defer tvUpdateCache.Unlock()
	c := tvUpdateCache.item
	if c.path == path && c.Size == st.Size() && c.modified.Equal(st.ModTime()) && c.VersionCode == meta.VersionCode && c.VersionName == meta.VersionName {
		return c, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return tvUpdate{}, err
	}
	defer f.Close()
	h := sha256.New()
	if n, err := io.Copy(h, f); err != nil || n != st.Size() {
		return tvUpdate{}, errors.New("read failed")
	}
	meta.SHA256, meta.Size, meta.path, meta.modified = hex.EncodeToString(h.Sum(nil)), st.Size(), path, st.ModTime()
	tvUpdateCache.item = meta
	return meta, nil
}

// Paired TVs and the owner only; a family share link has no use for it.
func (a *App) tvOrOwner(next http.HandlerFunc) http.HandlerFunc {
	return a.require(false, func(w http.ResponseWriter, r *http.Request) {
		if k := principalOf(r).Kind; k != "tv" && k != "owner" {
			fail(w, 403, "ใช้ได้เฉพาะทีวีที่จับคู่แล้ว")
			return
		}
		next(w, r)
	})
}

func (a *App) tvUpdateRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/tv/update", a.tvOrOwner(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.currentTVUpdate()
		if err != nil {
			fail(w, 404, "เครื่องกลางยังไม่มีไฟล์อัปเดตทีวี")
			return
		}
		jsonOut(w, 200, u)
	}))
	m.HandleFunc("GET /api/tv/update.apk", a.tvOrOwner(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.currentTVUpdate()
		if err != nil {
			fail(w, 404, "เครื่องกลางยังไม่มีไฟล์อัปเดตทีวี")
			return
		}
		f, err := os.Open(u.path)
		if err != nil {
			fail(w, 404, "เครื่องกลางยังไม่มีไฟล์อัปเดตทีวี")
			return
		}
		defer f.Close()
		h := w.Header()
		h.Set("Content-Type", "application/vnd.android.package-archive")
		h.Set("Content-Disposition", `attachment; filename="camerahub-tv-`+u.VersionName+`.apk"`)
		h.Set("X-CameraHub-SHA256", u.SHA256)
		http.ServeContent(w, r, "", u.modified, f)
	}))
}
