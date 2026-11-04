# เปิดดูกล้องจากนอกบ้านผ่าน Vercel — ขั้นตอนที่เจ้าของต้องทำเอง

หน้าเว็บอยู่บน Vercel แต่กล้อง วิดีโอ และฐานข้อมูลอยู่ที่เครื่อง Windows ที่บ้าน (port 17801) เบราว์เซอร์จากนอกบ้านจึงต้องเข้าถึงเครื่องบ้านผ่าน HTTPS ก่อน ทำตามลำดับนี้ครั้งเดียว

ที่ทำให้แล้ว: GitHub repo `https://github.com/laxmirung1970-hash/camerahub` (สาธารณะ ไม่มีข้อมูลส่วนตัว), Vercel project `camerahub` ผูกกับ repo (Root Directory `web`, build อัตโนมัติทุกครั้งที่ push `main`, ปิด preview build, ปิด Vercel Authentication เพื่อให้เปิดจากมือถือได้) และ backend รองรับ `APP_ALLOWED_ORIGINS` แล้ว

## 1. เปิด HTTPS ให้เครื่องบ้าน (เลือกอย่างเดียว)

### ทางเลือก A — Tailscale Funnel (ง่ายสุด ไม่ต้องมีโดเมน)

1. ติดตั้ง Tailscale บน Windows เครื่องกลาง แล้ว login (บัญชี Google/Microsoft ได้)
2. ใน https://login.tailscale.com/admin/dns เปิด **MagicDNS** และ **HTTPS Certificates**
3. ใน https://login.tailscale.com/admin/acls เพิ่ม node attribute `funnel` ให้เครื่องนี้ (หน้า admin มีปุ่ม "Enable Funnel" ครั้งแรกที่ใช้)
4. เปิด PowerShell (Run as Administrator) แล้วรัน

```powershell
tailscale funnel --bg 17801
```

5. คำสั่งจะพิมพ์ที่อยู่แบบ `https://<ชื่อเครื่อง>.<tailnet>.ts.net/` จดไว้ (เรียกว่า `<BACKEND>`) ทดสอบจากมือถือ (ปิด Wi-Fi บ้าน): เปิด `<BACKEND>/api/health` ต้องเห็น `{"mode":"live","status":"ok",...}`

ยกเลิกเมื่อไม่ใช้: `tailscale funnel --bg off 17801`

### ทางเลือก B — Cloudflare Tunnel (ถ้ามีโดเมนบน Cloudflare อยู่แล้ว)

1. Cloudflare Zero Trust → Networks → Tunnels → Create tunnel → ติดตั้ง `cloudflared` บน Windows ตามคำสั่งที่หน้าเว็บให้
2. Public hostname: `cam.<โดเมน>` → Service `http://127.0.0.1:17801`
3. `<BACKEND>` = `https://cam.<โดเมน>` ทดสอบ `/api/health` เหมือนข้อ A5

## 2. บอกเครื่องบ้านว่าให้รับคำขอจาก Vercel

แก้ไฟล์ `compose.windows-live.yaml` ใต้ `environment:` ของ `app` เพิ่ม 2 บรรทัด (คงบรรทัดเดิมไว้ทั้งหมด):

```yaml
      APP_ALLOWED_ORIGINS: "https://camerahub-<ชื่อทีม>.vercel.app"
      APP_SECURE_COOKIES: "true"
```

- ค่า origin ดูจากหน้า Vercel project → Domains (ที่ลงท้าย `.vercel.app` ตัวหลัก ไม่ใช่ตัวที่มีรหัสสุ่ม) ใส่ได้หลายค่าคั่นด้วยคอมมา ห้ามมี `/` ท้าย
- ถ้าใช้ Cloudflare Tunnel ให้เพิ่ม `APP_TRUSTED_PROXY: "127.0.0.1"` ด้วย (Tailscale Funnel ไม่ต้อง)

แล้วรัน `Start-CameraHub-Live.cmd` อีกครั้ง (port 17801 และข้อมูลเดิมไม่เปลี่ยน) ตรวจว่าได้ผล:

```powershell
curl.exe -s -X OPTIONS -H "Origin: https://camerahub-<ชื่อทีม>.vercel.app" -D - -o NUL http://127.0.0.1:17801/api/login
```

ต้องเห็นบรรทัด `Access-Control-Allow-Origin: https://camerahub-...` ถ้าไม่เห็น แปลว่า origin สะกดไม่ตรง

## 3. บอก Vercel ว่าเครื่องบ้านอยู่ที่ไหน

1. https://vercel.com → project **camerahub** → Settings → Environment Variables
2. Add: Key `VITE_API_BASE` Value `<BACKEND>` (เช่น `https://pc.example.ts.net` ไม่มี `/` ท้าย) เลือก Production
3. Deployments → เมนู ⋯ ของ deployment ล่าสุด → **Redeploy** (ค่านี้ฝังตอน build จึงต้อง build ใหม่ทุกครั้งที่เปลี่ยน)
4. เปิด `https://camerahub-<ชื่อทีม>.vercel.app` จากมือถือ ต้องเห็นหน้า login ไม่มีป้าย "ขาดการเชื่อมต่อเครื่องกลาง" ใส่รหัสเจ้าของเดิม

ถ้าเห็นป้ายขาดการเชื่อมต่อ: เปิด `<BACKEND>/api/health` บนมือถือก่อน (ข้อ 1 ผ่านไหม) แล้วดู Console ของเบราว์เซอร์ ถ้าเป็น CORS error = ข้อ 2 ยังไม่ตรง

## 4. ตรวจกล้องจริงบน 17801 หลังอัปเดต (ทำได้เลย ไม่ต้องรอข้อ 1–3)

1. เปิด http://127.0.0.1:17801 ใส่รหัสเจ้าของ
2. ดูว่า 4 ช่องแรกขึ้นภาพและป้าย LIVE ภายใน ~15 วินาที ลองเปลี่ยนโหมด "วน 4" / "ล็อก 4" และเปิดแผงรายละเอียดกล้องหนึ่งตัว (คลิกภาพ) ภาพช่องอื่นต้องเล่นต่อ
3. ตั้งค่า (⚙) → แท็บ "เชื่อมกล้อง" ต้องเห็นบัญชี Tapo/Xiaomi เดิมครบ

ถ้ามีปัญหา ย้อนกลับรุ่นก่อนได้ทันที (ข้อมูลไม่หาย):

```powershell
docker tag camerahub/app:before-backend-20261104 camerahub/app:1.0.0-amd64
```

แล้วรัน `Start-CameraHub-Live.cmd` (ย้อนไปก่อนเว็บใหม่ทั้งหมดใช้ `before-redesign-20261104` แทน)

## ข้อจำกัด

- iPhone Safari เล่น HLS แบบ native ส่ง token ข้ามโดเมนไม่ได้ ให้ใช้ Chrome บน iPhone หรือเปิดผ่าน `<BACKEND>` โดยตรง (โดเมนเดียวกับ backend ใช้ cookie ได้ปกติ) หน้า Vercel เหมาะกับ Android/Windows/TV
- Tailscale Funnel จำกัดแบนด์วิดท์ ดูพร้อมกันหลายช่องอาจกระตุก ใช้คุณภาพ "ต่ำ" (ค่าเริ่มต้น)
- Dependabot จะเปิด PR อัปเดต dependency บน GitHub เป็นระยะ merge ได้เมื่อ CI เขียว ไม่ merge ก็ไม่กระทบระบบที่บ้าน
