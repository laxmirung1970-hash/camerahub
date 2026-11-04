package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticHandlerServesShellAssetsAndNothingElse(t *testing.T) {
	web := t.TempDir()
	_ = os.MkdirAll(filepath.Join(web, "assets"), 0700)
	_ = os.MkdirAll(filepath.Join(web, "fonts"), 0700)
	_ = os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>shell"), 0600)
	_ = os.WriteFile(filepath.Join(web, "assets", "app.js"), []byte("js"), 0600)
	_ = os.WriteFile(filepath.Join(web, ".secret"), []byte("private"), 0600)
	h := staticHandler(web)
	for _, tc := range []struct {
		path, body, cache string
		status            int
	}{
		{"/", "shell", "no-cache", 200},
		{"/cameras/abc", "shell", "no-cache", 200},
		{"/assets/app.js", "js", "immutable", 200},
		{"/assets", "shell", "no-cache", 200},
		{"/fonts/", "shell", "no-cache", 200},
		{"/missing.js", "", "", 404},
		{"/.secret", "", "", 404},
		{"/assets/.hidden", "", "", 404},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) || !strings.Contains(w.Header().Get("Cache-Control"), tc.cache) {
			t.Fatalf("%s: %d %q %q", tc.path, w.Code, w.Body.String(), w.Header().Get("Cache-Control"))
		}
		if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "<pre>") {
			t.Fatalf("%s leaked a listing or dot-file", tc.path)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 405 {
		t.Fatal("static route accepted a POST")
	}
}
