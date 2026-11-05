package main

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

const appVersion = "1.0.0"

// Process configuration is read once at startup. Handlers never consult the
// environment, so tests set variables before newApp and behaviour is uniform.
type config struct {
	webRTC, secureCookies                       bool
	gatewayConfig, relayHostsFile, trustedProxy string
	// Exact origins allowed to call the API with a Bearer token from another
	// site (the Vercel-hosted UI). Empty keeps same-origin cookies only.
	allowedOrigins map[string]bool
	// Folder with the TV APK the hub offers paired TVs (tv_update.go).
	tvUpdateDir string
}

func env(k, f string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return f
}
func loadConfig() config {
	return config{
		webRTC:         env("APP_WEBRTC", "false") == "true",
		secureCookies:  env("APP_SECURE_COOKIES", "false") == "true",
		gatewayConfig:  os.Getenv("GATEWAY_CONFIG"),
		relayHostsFile: os.Getenv("CAMERAHUB_RELAY_HOSTS_FILE"),
		trustedProxy:   os.Getenv("APP_TRUSTED_PROXY"),
		allowedOrigins: parseOrigins(os.Getenv("APP_ALLOWED_ORIGINS")),
		tvUpdateDir:    env("APP_TV_UPDATE_DIR", "/tv-update"),
	}
}

// Comma-separated exact origins. Wildcards, paths and credentials are ignored.
func parseOrigins(raw string) map[string]bool {
	out := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		if origin := normalizeOrigin(strings.TrimSpace(item)); origin != "" {
			out[origin] = true
		}
	}
	return out
}
func normalizeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return ""
	}
	return scheme + "://" + strings.ToLower(u.Host)
}
func (a *App) crossOriginAllowed(r *http.Request) bool {
	origin := normalizeOrigin(r.Header.Get("Origin"))
	return origin != "" && a.cfg.allowedOrigins[origin]
}
