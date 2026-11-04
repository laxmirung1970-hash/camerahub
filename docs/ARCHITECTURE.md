# CameraHub development notes

## Processes and state

`React PWA / Kotlin TV → authenticated Go API → go2rtc → camera`

The Go process serves static UI, auth, SQLite, discovery workers, metadata, PTZ and an allowlisted media proxy. go2rtc is private to Docker's network on Windows and loopback on the NAS. API clients never receive original RTSP URLs or camera passwords. TV receives a device-scoped token; owner-only setup, credentials, backup and device-management endpoints reject that token.

Demo and live use distinct SQLite databases and 32-byte encryption keys. Demo seeds 2 Tapo + 13 Xiaomi records. Video fixtures are original synthetic 12-second looping H.264/AAC clips with a moving target and quiet tones, not recordings from real cameras. `Source` is AES-GCM encrypted in SQLite. The storage key is in a mode-specific 0600 file under a 0700 directory. go2rtc's token/config directory is also private. Never include either directory in a source/archive release.

Owner passwords use Argon2id. Sessions are random 256-bit tokens, stored hashed in SQLite, 14-day owner/90-day TV/365-day share expiry. A share session (`kind=share`, `sessions.cameras` JSON allowlist) is the credential behind a family link `https://<lite UI>/#<token>`: it lists and streams only its cameras (`host`/`ref` stripped), gets 404 for any other camera id, 403 for PTZ and every owner route, and is revoked like a TV from `/api/devices`. `GET /api/media/hls/{file}` cannot map a go2rtc HLS session back to a camera, so a share token can fetch segments of an HLS session whose id it learned elsewhere; the same limit applies to TV tokens. Cookies are HttpOnly/SameSite Strict; NAS enables Secure cookies. Mutating browser requests reject foreign Origin/Sec-Fetch-Site. Pairing codes expire in five minutes and require an authenticated owner; delivery also requires a separate 256-bit polling secret and happens once. Device revocation invalidates subsequent API/media requests; an already buffered video chunk is not remotely erased.

Cross-origin UI (the Vercel-hosted build) is opt-in. With `APP_ALLOWED_ORIGINS` unset the server is byte-identical to the cookie-only behaviour above: no `Access-Control-*` headers, foreign `Origin`/`Sec-Fetch-Site: cross-site` mutations rejected. When set (comma-separated exact origins, e.g. `https://camerahub.vercel.app`), those origins get an exact `Access-Control-Allow-Origin` echo with credentials, a 204 preflight, and `POST /api/setup` / `POST /api/login` return `{"token"}` instead of setting a cookie. The token is sent as `Authorization: Bearer` on fetch and HLS requests and as the WebSocket subprotocol pair `Sec-WebSocket-Protocol: bearer, <token>` (never a query string); the server echoes `bearer`. Same-origin clients may also ask for a token with `{"token":true}`. `APP_TRUSTED_PROXY=<ip>` makes rate limiting read the rightmost `X-Forwarded-For` entry only when the connection comes from that proxy. Configuration is read once at startup (`backend/config.go`).

Windows manual-IP live entry point: `compose.windows-live.yaml` uses the same amd64 images as demo with `APP_MODE=live`, distinct volumes under `camerahub-windows-live`, a private gateway network and only `127.0.0.1:17801` published. HTTP is limited to the local PC; NAS keeps HTTPS/Secure cookies. The launcher never enables discovery or configures an owner/camera. `Start-CameraHub-Live.cmd` displays the one-time bootstrap code to the local user and opens the UI. Camera/network compatibility remains a field test.

## Public endpoints

| Method / path | Purpose | Access |
|---|---|---|
| GET `/api/bootstrap`, `/api/health` | mode, setup/login state; process health | public, no secrets |
| POST `/api/setup`, `/api/login` | bootstrap-code owner setup; password login | rate limited |
| POST `/api/logout` | revoke caller session | authenticated |
| GET `/api/cameras` | metadata and capability/verification flags; a share link sees only its cameras, without `host`/`ref` | owner / TV / share |
| PATCH `/api/cameras/{id}` | label, room, favorite, playback/PTZ profile, manual field verification | owner |
| POST `/api/cameras/{id}/probe` | negotiate a real video consumer; does not certify audio/PTZ | owner |
| POST `/api/cameras/{id}/ptz` | `{direction:left/right/up/down/stop}` | owner / TV |
| GET `/api/cameras/{id}/ws` | authenticated MSE WebSocket; allowlisted negotiation messages | owner / TV / share (scoped) |
| POST `/api/cameras/{id}/webrtc` | SDP proxy; separate Opus audio profile | owner / TV / share (scoped) |
| GET `/api/cameras/{id}/hls` | HLS master playlist, `quality=low/high` | owner / TV / share (scoped) |
| GET `/api/media/hls/{file}` | allowlisted playlists/segments, rewritten local paths | owner / TV / share |
| GET/POST `/api/connections` | encrypted credential config and enabled state | owner |
| POST `/api/connections/{id}/toggle`, `/api/discover` | explicitly enable/pause or run discovery | owner |
| POST `/api/cameras/tapo` | manual private-IP ONVIF setup | owner, live only |
| GET `/api/xiaomi/accounts`, POST `/api/xiaomi/auth` | local gateway login, CAPTCHA/OTP continuation | owner, live only |
| POST `/api/pair/request`, `/api/pair/poll` | request/wait for owner-approved TV pairing | public, rate limited + secret |
| POST `/api/pair/approve`, GET `/api/pair/pending` | authorize a displayed code | owner |
| GET `/api/devices`, DELETE `/api/devices/{id}` | list/revoke paired TVs and share links (`kind`, `cameras`) | owner |
| POST `/api/shares` | `{name, cameras:[id…]}` → view-only token scoped to 1–8 cameras, returned once; 365-day expiry | owner |
| GET `/api/system`, PUT `/api/system/remote` | actual gateway health, app memory/events; saved HTTPS address | owner |
| POST `/api/backup`, `/api/backup/restore` | scrypt + AES-GCM encrypted snapshot | owner |
| POST `/api/demo/cameras`, `/api/demo/cameras/{id}/scenario` | grow registry / inject offline or auth expiry | owner, demo only |

## Connectors

- Tapo: UDP WS-Discovery is opt-in via enabled connection; manual IP fallback. SOAP uses WSSE PasswordDigest without duplicate HTTP Basic credentials, GetDeviceInformation/GetCapabilities/GetProfiles/GetStreamUri. Service/RTSP hosts must match the configured private camera IP. Serial number gives identity across DHCP changes. ONVIF ContinuousMove requests have a 0.6-second device timeout; an 0.8-second server lease, explicit Stop, disconnect/window blur/app pause and process shutdown provide additional stop paths. Setup errors expose only operation, HTTP status and allowlisted fault categories; never a camera response body or credentials.
- Xiaomi: pinned go2rtc cloud source for listing/keys and local P2P streaming. Account+device ID gives identity across IP changes. Login challenges are serialized and ordered to avoid invalid gateway states. Cloud credentials are not sent to browsers. New camera discovery leaves verification untested. MISS step PTZ checks the patched gateway's actual `{reply:JSON,hex:...}` envelope and requires `ret:0`. It tries the active low/high original profile; continuous legacy motor protocols are not claimed as supported.
- Demo: shares API and go2rtc media path but PTZ explicitly returns `simulated:true`; offline/expired fixtures refuse playback/control. No production credentials accepted.

Cloud discovery upserts rather than deleting missing devices; preserves aliases, favorites, rooms and selected compatibility profile. Xiaomi cloud listing is model/account/region dependent and can omit shared or older devices. A missing device must be investigated, not automatically hidden. A successful stream probe changes `verification` to `stream_only`; sound and PTZ are manually confirmed separately.

## Media, packaging and known limits

MSE is the default private-HTTPS path; HLS is the TV/native fallback. WebRTC is implemented but opt-in until ICE/network routing is field-tested. Opus alternatives live in a separate stream, so HLS cannot accidentally choose an audio-only producer. Compatibility mode registers an original stream plus one shared FFmpeg H.264/AAC derivative per camera/quality. It is never enabled silently; DS223 CPU limits remain unmeasured.

Video transport is cleaned up on unmount, quality changes and background visibility. Opening detail pauses grid transports. A progress watchdog attempts reconnection on stalled video. HLS is lazy loaded and receives auth on every request. Service worker never caches APIs, video or secrets.

BuildKit cross-compiles Go for the requested `TARGETARCH`. ARM runtime files are assembled from the signed target Alpine base/packages with `apk --root --no-scripts` on the native builder; no ARM binary execution or privileged binfmt registration is needed. ARM64 image metadata and ELF architecture are checked separately. Windows runtime tests do not certify ARM execution on DS223.

Third-party source pins and licenses: `vendor/`, `docs/THIRD_PARTY.md`. Build tags are local, not pushed to a registry. Android is minSdk 26/target 35 with Media3; the APK is personally signed. Actual decoder count, focus behavior, motion and lifecycle on Mi TV/Android TV remain field tests.

## Run checks

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/Test-Backend.ps1
Set-Location web
npm ci
npm run build
Set-Location ..
node scripts/integration.mjs
powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/Build-TV.ps1
```

The integration script only runs against demo and refuses a previously user-configured owner. Its fixture access file is under ignored `data/`. Browser QA must use the supported computer-use browser; no page scripts may mutate state outside UI actions.
