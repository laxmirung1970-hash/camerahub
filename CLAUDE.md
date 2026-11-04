# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

CameraHub: self-hosted viewer for the owner's real Tapo + Xiaomi cameras (web PWA, Android TV app), running on a Windows PC today and intended for a Synology DS223 (ARM64, 2 GB RAM) NAS. Public GitHub repo; `private/` is gitignored and holds the owner-only session log and field reports. User-facing docs and status notes are in Thai; the owner communicates in Thai.

**Read `private/NEXT_SESSION.md` (top section) before starting work.** It is the running log of what is deployed, which image is live, rollback tags, which cameras currently fail, and what has *not* been verified. README "ล่าสุด" banners summarise the same. Older sections are history — don't treat them as current.

## Commands

No Go toolchain on this machine; backend builds/tests run in Docker. Node is installed.

```powershell
# Backend: gofmt check, go vet, go test -race inside golang:1.25 (Debian image; race needs cgo)
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/Test-Backend.ps1
# Single Go test (same container, custom command)
docker run --rm --mount "type=bind,source=${PWD}\backend,target=/src" --mount type=volume,source=camerahub-gomod,target=/go/pkg/mod --mount type=volume,source=camerahub-gocache,target=/root/.cache/go-build --workdir /src golang:1.25-alpine go test -run TestName -v ./...

# Web (web/)
npm ci
npm run build          # tsc -b && vite build
npm test               # vitest run
npx vitest run src/reconnectPolicy.test.ts   # single file
npm run lint           # eslint src (CI runs lint → test → build)
npm run dev            # Vite on 127.0.0.1, proxies /api to demo app on :17800

# Node script tests (Windows UDP relay, private-data scanner)
node --test scripts/windows-udp-relay.test.mjs
node --test scripts/check-private.test.mjs

# Demo-only API/ffprobe integration check (refuses live mode / user-configured owner)
node scripts/integration.mjs

# Android TV: unit tests + lint + signed APK + sha256, built inside Docker
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/Build-TV.ps1
# NAS ARM64 images (cross-build, no emulation)
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/Build-NAS.ps1
```

Running modes:
- **Demo** (synthetic cameras, `127.0.0.1:17800`): `scripts/Start-Demo.ps1 [-Rebuild]`, stop with `scripts/Stop-Demo.ps1`.
- **Live** (owner's real cameras, `127.0.0.1:17801`): `Start-CameraHub-Live.cmd` → `scripts/Start-Live.ps1`, stop with `scripts/Stop-Live.ps1`. Status: `scripts/Check-System.ps1 -Mode windows-live`. A per-user scheduled task "CameraHub Live Watchdog" (`scripts/Install-LiveWatchdog.ps1`, installed on this PC) runs `scripts/Watch-Live.ps1` every 5 min to restart the Node UDP relay if it stops answering while the live app container runs; its log is `data/windows-live-gateway/watchdog.log`.

Both use image tag `camerahub/app:1.0.0-amd64`; redeploying the app means rebuilding that image (`docker build -t camerahub/app:1.0.0-amd64 .`) and restarting the compose app service. Before replacing the live image, tag the current one for rollback (convention: `camerahub/app:before-<change>-<yyyymmdd>`) and record it in `private/NEXT_SESSION.md`.

## Architecture

`React PWA / Kotlin TV → authenticated Go API (backend/) → go2rtc gateway → camera`

- **backend/** (Go, package `main`, SQLite via modernc, no CGO): one process serves the built web UI, auth (Argon2id owner, hashed session tokens, device-scoped TV tokens), discovery workers, camera registry, PTZ, and an allowlisted proxy to go2rtc (MSE WebSocket, HLS, WebRTC SDP). Clients never see RTSP URLs or camera credentials; camera `Source` is AES-GCM encrypted in SQLite with a per-mode key file. `APP_MODE=demo|live` selects separate DB/keys. Endpoint table and connector rules are in `docs/ARCHITECTURE.md`.
  - `adapters.go`/`onvif.go`/`ptz.go`: Tapo via ONVIF (WS-Discovery opt-in, manual IP, ContinuousMove with device timeout + server lease + explicit stop).
  - `bridge.go`, `xiaomi_ptz.go`: Xiaomi via patched go2rtc (cloud for listing/keys, local P2P/CS2 for media; MISS step PTZ requires `ret:0` in the gateway reply envelope).
  - `xiaomi_dual.go`: C300 Dual expands one registry entry into two lens channels (`parentId/lens/lensCount`, channel=2); motor control stays on the primary channel. Startup migration is idempotent.
  - `store.go`: discovery **upserts, never deletes**; preserves owner aliases, order, rooms, favorites, profiles. `lastSeen` from video must not be overwritten by inventory sync.
- **Gateway**: go2rtc pinned to a commit and built from source in `deploy/go2rtc.Dockerfile`, with sha256-checked patches from `vendor/` (Xiaomi control, motor replies, startup, Windows UDP relay). Changing a patch requires updating its hash in the Dockerfile.
- **Windows live media path**: Go networking on this Windows host hangs on outgoing TCP, and Docker Desktop NAT breaks Xiaomi UDP. Current runtime is `docker-relay`: Linux go2rtc in Docker (`compose.windows-live.yaml` + `compose.windows-relay.yaml`) plus the authenticated Node UDP relay `scripts/windows-udp-relay.mjs` on `127.0.0.1:17993`, only forwarding CS2 UDP to camera IPs in the registry (`relay_hosts.go`). `compose.windows-native.yaml` (native Windows go2rtc) is the older alternative. `Start-Live.ps1` chooses based on state under `data/windows-live-gateway/`.
- **Cross-origin (Vercel) mode, opt-in**: default is same-origin (Windows live, NAS) with cookie auth. A web build with `VITE_API_BASE=<backend https origin>` (`web/src/api.ts`, `web/vercel.json`) calls the home backend with a Bearer token; the backend only allows it for exact origins in `APP_ALLOWED_ORIGINS` (`config.go`, `auth.go`). Cookie mutations still reject cross-site requests.
- **CI** (`.github/workflows/ci.yml`): private-data scan, web lint/test/build, backend gofmt/vet/`test -race` on native Go 1.25, relay test. Mirror these locally before pushing.
- **web/** (React 19 + TS + Vite, Thai UI, IBM Plex Sans Thai): `App.tsx` holds a single display state; pure transitions in `displayState.ts` (tour 4 / tour big / lock 4 / focus 1, pages of 4). `Player.tsx` handles MSE→HLS fallback; `reconnectPolicy.ts` retries forever while visible (10/20/40s then ~60s + jitter) and pauses on auth rejection. Errors render as black tile with reason; LIVE badge only when frames are actually playing. Service worker must never cache API/video.
- **android-tv/** (Kotlin, Media3, minSdk 26/target 35, appId `local.camerahub.tv`): remote-driven 2×2 grid; state/view/playback/PTZ split into `TvViewingState`, `TvMonitorView`, `TvPlayback`, `TvPtzController`. Max 4 players; one PTZ request in flight, stop before direction/camera change.
- **deploy/**: NAS compose (HTTPS, Secure cookies, loopback go2rtc). ARM runtime is assembled with `apk --root --no-scripts` so no ARM binaries execute on the build host.

## Rules specific to this project

- **Privacy of data**: `private/`, `data/`, `.cache/`, `runtime/windows/`, keystores and `*.cambackup` hold real credentials, keys, camera IPs/DIDs, footage and screenshots. Never include them in releases (`scripts/Package-Release.ps1`), tracked docs, or chat. Tracked docs and tests use placeholder addresses (`192.0.2.x` in docs, `172.16.0.x`/`10.x` in code fixtures) and `tapo-user` names, never the real ones. Run `node scripts/check-private.mjs` before any push (the `.githooks/pre-push` hook does this once `git config core.hooksPath .githooks` is set).
- **Don't act for the owner on cameras**: no reset/reboot/firmware update, no credential guessing, no new account linking, no enabling discovery. Don't set `AudioVerified`/`PTZVerified` — only a successful probe sets `verification: stream_only`; sound and PTZ are confirmed by the owner. Don't hide or delete cameras that fail.
- **Live system is in use**: keep port 17801, existing volumes, and owner-edited names/order. Default view must keep cycling through *all* cameras (tour 4 / 30s), not a test subset.
- Nothing is exposed publicly; don't publish ports to LAN/internet. Phone/TV access is meant to go through NAS HTTPS/Tailscale (not yet set up).
- Report honestly what was field-verified vs. only unit/build-tested. Releases (`releases/` ZIP/ARM64 tar, APK 1.0.0) are stale relative to current source unless `private/NEXT_SESSION.md` says otherwise. Known open issues: Tapo91 unreachable, IndoorReceiver unconfirmed, TV/NAS/real phone untested.
- After a change, update the top of `private/NEXT_SESSION.md` (and a dated `docs/*.th.md` report for significant work) in Thai, matching the existing style.
