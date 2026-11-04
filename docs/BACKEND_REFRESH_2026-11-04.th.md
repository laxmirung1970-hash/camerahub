# Backend รอบ 4 พฤศจิกายน 2026 — แก้บั๊ก ลดโค้ดซ้ำ และ CORS/Bearer แบบเลือกเปิด

ต่อจากงานเว็บ Minimal Dark ช่วงบ่าย (`docs/WEB_REFRESH_2026-11-04.th.md`) รอบนี้แตะเฉพาะ `backend/` และสคริปต์ทดสอบ ไม่เปลี่ยน schema ของ SQLite ไม่เปลี่ยน API ที่เว็บ/ทีวีใช้อยู่ (เพิ่มเฉพาะช่อง `token` ใน login/setup) ค่าเริ่มต้นเมื่อไม่ตั้ง `APP_ALLOWED_ORIGINS` ทำงานเหมือนเดิมทุกประการ Windows live 17801 ไม่ต้องตั้งตัวแปรใหม่

## บั๊กที่แก้ (14 ข้อ)

1. **ปิดโปรแกรมแล้วฐานข้อมูลปิดก่อน worker** `main.go` — `serve()` รัน worker + HTTP แยก goroutine รอทั้งคู่จบก่อน `DB.Close()` เพิ่ม `ReadTimeout 30s`/`WriteTimeout 2m` (WebSocket ถูก hijack ไม่โดน)
2. **worker ค้างรอ gateway** `background` เดิมไม่เริ่ม ticker ล้าง session/ซิงก์จนกว่า gateway พร้อม — แยกเป็น `registerAllStreams` (ลองใหม่ทุก 2 วินาที) + `cleanupExpired` + `scheduledSync` ที่เริ่มทันที
3. **แก้ชื่อระหว่างตรวจภาพแล้วชื่อหาย** — `Store.updateCamera(id, fn)` ล็อก `writeMu` อ่าน-แก้-บันทึกเฉพาะฟิลด์ของตัวเอง ใช้ใน PATCH/probe/ptz probe/demo scenario; `mergeCamera`/`mergeDiscoveredCameras`/restore ล็อกตัวเดียวกัน (ทดสอบ: probe ค้างที่ WS ระหว่าง PATCH ชื่อ → ได้ทั้งชื่อใหม่และผลตรวจ)
4. **rate limit หลัง proxy** — `APP_TRUSTED_PROXY` อ่าน `X-Forwarded-For` ตัวขวาสุดเฉพาะเมื่อ RemoteAddr คือ proxy นั้น และล้าง bucket เก่าทุกนาที (เดิมล้างเฉพาะเมื่อเกิน 2000)
5. **logout / ยกเลิกทีวี DELETE ล้มแต่ตอบ ok** — ตรวจ error ตอบ 500 `ออกจากระบบไม่สำเร็จ` / `ยกเลิกอุปกรณ์ไม่สำเร็จ`
6. **ลงทะเบียน stream ถือ mutex ระหว่าง I/O** `bridge.register` — ล็อกต่อชื่อ stream (`inflight`) I/O อยู่นอก `mu` กล้องช้าตัวเดียวไม่บล็อกตัวอื่น stream เดียวกันยังส่ง PUT ครั้งเดียว มี `resetCache()`
7. **ซิงก์อัตโนมัติไม่มี timeout** — `scheduledSync(ctx, vendor, 60s)`
8. **restore รับไฟล์สำรองโดยไม่ตรวจแหล่งวิดีโอ** `backup.go` — แยก `decodeBackup`/`validateBackup`/`applyBackup`: live tapo ต้องเป็น `rtsp://` host เดียวกับกล้อง IP ส่วนตัว และ URL ONVIF ต้องชี้กล้องเดียวกัน; xiaomi ต้อง `xiaomi://` มี did + IP ส่วนตัว; demo ต้อง `ffmpeg:/media/` ห้าม `exec`/`|`; ตรวจ connection/region/account/remoteURL/ID ซ้ำ **commit ฐานข้อมูลก่อน** ค่อยเขียนไฟล์ gateway (เดิมเขียนไฟล์ก่อน commit ถ้า commit ล้ม gateway ใช้ config ใหม่แต่ทะเบียนเก่า) แล้ว `resetCache` + `writeRelayHosts`
9. **บัญชี Tapo สองบัญชีทับกัน** — `connectionID("tapo", …)` ใช้ username (ตัวพิมพ์เล็ก) เดิมทุกบัญชี Tapo ได้ ID เดียวกัน; อ่าน connections ล้ม → 500
10. **PUT /api/cameras/order อ่าน body ขณะถือ syncMu** — อ่าน JSON ก่อนล็อก
11. **error ที่ไม่เคยตรวจ** — `lookupSetting` แยก "ไม่มีค่า" จาก "ฐานข้อมูลพัง" (setup ตอบ 500 แทน 403 เมื่อตาราง settings หาย), `event()` log เมื่อเขียนไม่ได้, `saveConnection` ล้มระหว่างซิงก์นับเป็นล้ม + event
12. **จำกัดความยาวนับ byte** — ชื่อกล้อง 180 / กลุ่ม 120 / ทีวี 80 / เจ้าของ 2–80 / ชื่อการเชื่อมต่อ 120 นับเป็นตัวอักษร (ภาษาไทย 1 ตัว = 3 byte เดิมชื่อไทย 61 ตัวโดนปฏิเสธ) trim ชื่อเจ้าของครั้งเดียวแล้วใช้ค่าเดียวกันกับ session
13. **PRAGMA ใช้กับ connection แรกเท่านั้น** — ย้าย `busy_timeout`/`journal_mode=WAL`/`foreign_keys` ไปใน DSN
14. **เสิร์ฟไฟล์เว็บ** `staticHandler` — `path.Clean`, ปฏิเสธ dot-file, ไม่ list โฟลเดอร์, `/assets/*` = `immutable` 1 ปี, SPA fallback เฉพาะ path ไม่มีนามสกุล

## ลดโค้ดซ้ำ

- `config.go`: อ่าน env ครั้งเดียวใน `newApp` (`webRTC`, `secureCookies`, `gatewayConfig`, `relayHostsFile`, `trustedProxy`, `allowedOrigins`) handler ไม่อ่าน env อีก
- `store.go`: `execer` ใช้ได้ทั้ง `*sql.DB`/`*sql.Tx`, `upsertCamera`/`upsertConnection`/`upsertSetting`/`decodeCamera`/`decodeConnection` ใช้ร่วมใน saveCamera, mergeDiscoveredCameras, applyBackup, setup (ตัด SQL ซ้ำ 5 จุด)
- helper: `cameraFromPath`, `cameraUnavailable`, `liveOnly`, `demoOnly`, `limit` (rate limit + 429), `ok`, `runes`, `appVersion`, `wsMessage`, `principalOf(r)` จาก context (เลิก query session ซ้ำใน handler), `validRemoteURL`, `demoCamera`
- แยก handler ใหญ่: `cameraRoutes` → listCameras/patchCamera/probeCamera/addDemoCamera/demoScenario; `mediaRoutes` → webrtcHandler/wsHandler (+pumpToGateway/pumpToClient)/hlsPlaylistHandler/hlsFileHandler; `connectionRoutes` → 7 handler; `xiaomiAuth` → xiaomiAuthForm + xiaomiChallenge + xiaomiAccountIDs; `mergeDiscoveredCameras` → `placeNewLenses` (pure, มี table test)
- ลบ `parseInt`, `decodeSource`, `safeError`, request ไร้ผลใน test
- ONVIF (`onvif.go`) ไม่แตะ — ไม่มีกล้องให้ทดสอบ

## CORS / Bearer (เลือกเปิด)

- `APP_ALLOWED_ORIGINS=https://a.vercel.app,https://b.example` ต้องตรงทั้ง scheme+host ไม่รับ wildcard/path
- origin ที่อนุญาตได้ `Access-Control-Allow-Origin` ตรงตัว + `Allow-Credentials` + `Vary: Origin` + `Expose-Headers: Content-Disposition`; `OPTIONS` ตอบ 204 พร้อม Allow-Methods/Headers (`Authorization`)/Max-Age 600; ข้ามการปฏิเสธ Origin/Sec-Fetch-Site เฉพาะ origin ในรายการ
- login/setup จาก origin ที่อนุญาต: ไม่ตั้ง cookie ตอบ `{"ok":true,"token":"…"}`; same-origin ขอได้ด้วย `{"token":true}` (ยังได้ cookie)
- WebSocket: token ส่งใน `Sec-WebSocket-Protocol: bearer, <token>` server echo `bearer`; ไม่รับ query string; `CheckOrigin` = ว่าง/host เดียวกัน/ในรายการ
- HLS ใช้ Bearer ได้อยู่แล้ว ทดสอบว่า header CORS ถึง `/api/cameras/{id}/hls`
- ตรงกับฝั่งเว็บ (`web/src/api.ts`, `transport.ts`) ที่ทำไว้แล้ว

## ทดสอบ

`scripts/Test-Backend.ps1` ตอนนี้ = gofmt ต้องว่าง + `go vet` + `go test -race` ใน `golang:1.25` (Debian มี gcc สำหรับ race) ผ่านทั้งหมด 47 test (เดิม 28) ไฟล์ใหม่: `fixes_test.go`, `cors_test.go`, `backup_test.go`, `static_test.go`, `main_test.go` โค้ด `backend/*.go` รวม 5,391 บรรทัด (เดิม 4,309 — เพิ่มจาก test ~900 บรรทัด โค้ดจริงลดลงเล็กน้อยแม้เพิ่ม config/validation)

ไม่ได้ทดสอบ: กล้องจริง/ONVIF/Xiaomi cloud, Vercel end-to-end (ยังไม่มี tunnel), NAS
