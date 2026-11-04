import { useState } from "react";
import { Copy, Link, Tv } from "lucide-react";
import { api } from "../api";
import { buildShareLink } from "../lite/shareLink";
import { Notice } from "../ui";
import type { TabProps } from "./SettingsModal";

const SHARE_ORIGIN = import.meta.env.VITE_SHARE_ORIGIN ?? "";

export function DevicesTab({ cameras, data, busy, action, notify }: TabProps) {
  const [picked, setPicked] = useState<string[]>([]);
  const [shareName, setShareName] = useState("");
  const [link, setLink] = useState("");
  const togglePick = (id: string) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link);
      notify("คัดลอกลิงก์แล้ว");
    } catch {
      notify("คัดลอกอัตโนมัติไม่ได้ กดค้างที่ลิงก์เพื่อคัดลอกเอง", "error");
    }
  };
  return (
    <div className="stack">
      <Notice>เปิดแอป CameraHub บนทีวี กรอกที่อยู่เครื่องกลาง แล้วนำรหัสจับคู่มาอนุมัติที่นี่ ทีวีไม่ต้องรู้รหัสผ่านเจ้าของ</Notice>
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          const f = new FormData(e.currentTarget);
          void action(async () => {
            await api("/pair/approve", { code: f.get("code") });
            notify("อนุมัติทีวีแล้ว");
          });
        }}
      >
        <input name="code" required placeholder="รหัสบนหน้าจอทีวี" autoComplete="off" aria-label="รหัสจับคู่" />
        <button className="btn primary" disabled={busy}>
          อนุมัติทีวี
        </button>
      </form>
      {data.pending.map((p) => (
        <div className="row between card" key={p.code}>
          <span>
            <Tv size={18} /> <strong>{p.name}</strong> <small className="muted">รอการอนุมัติ</small>
          </span>
          <code>{p.code}</code>
        </div>
      ))}

      <h3>ลิงก์แชร์ดูอย่างเดียว</h3>
      <Notice>
        ลิงก์สำหรับคนในครอบครัว เปิดแล้วเห็นเฉพาะกล้องที่เลือก ไม่ต้องล็อกอิน สั่งหมุนกล้องไม่ได้ ใครมีลิงก์นี้ดูกล้องที่เลือกได้
        เก็บเหมือนรหัสผ่าน ยกเลิกได้ที่รายการด้านล่าง
      </Notice>
      <form
        className="stack"
        onSubmit={(e) => {
          e.preventDefault();
          void action(async () => {
            const out = await api<{ token: string }>("/shares", { name: shareName.trim(), cameras: picked });
            setLink(SHARE_ORIGIN ? buildShareLink(SHARE_ORIGIN, out.token) : out.token);
            setPicked([]);
            setShareName("");
            notify("สร้างลิงก์แชร์แล้ว");
          });
        }}
      >
        <input
          required
          maxLength={80}
          placeholder="ชื่อลิงก์ เช่น ห้องอาม่า"
          aria-label="ชื่อลิงก์แชร์"
          value={shareName}
          onChange={(e) => setShareName(e.target.value)}
        />
        <div className="stack share-cameras" role="group" aria-label="เลือกกล้องที่แชร์">
          {cameras.length === 0 && <small className="muted">ยังไม่มีกล้องให้เลือก</small>}
          {cameras.map((c) => (
            <label className="row" key={c.id}>
              <input type="checkbox" checked={picked.includes(c.id)} onChange={() => togglePick(c.id)} />
              <span>
                {c.name} {c.group && <small className="muted">· {c.group}</small>}
              </span>
            </label>
          ))}
        </div>
        <div className="row">
          <button className="btn primary" disabled={busy || picked.length === 0 || picked.length > 8}>
            <Link size={16} /> สร้างลิงก์แชร์
          </button>
          <small className="muted">เลือกได้ 1–8 กล้อง</small>
        </div>
      </form>
      {link && (
        <div className="card stack">
          <small className="muted">
            {SHARE_ORIGIN ? "ส่งลิงก์นี้ให้ครอบครัว แสดงครั้งเดียว ปิดหน้านี้แล้วจะไม่เห็นอีก" : "ยังไม่ได้ตั้งค่าที่อยู่แอปเล็ก (VITE_SHARE_ORIGIN) นำรหัสนี้ไปต่อท้าย https://<แอปเล็ก>/# เอง"}
          </small>
          <code className="share-link" data-testid="share-link">
            {link}
          </code>
          <button className="btn small" type="button" onClick={() => void copy()}>
            <Copy size={15} /> คัดลอก
          </button>
        </div>
      )}

      <h3>อุปกรณ์และลิงก์ที่ใช้งานอยู่</h3>
      {data.devices.length === 0 && <small className="muted">ยังไม่มีทีวีที่จับคู่หรือลิงก์แชร์</small>}
      {data.devices.map((d) => (
        <div className="row between card" key={d.id}>
          <span>
            {d.kind === "share" ? <Link size={18} /> : <Tv size={18} />} <strong>{d.name}</strong>
            {d.kind === "share" && <small className="muted"> · กล้อง {d.cameras?.length ?? 0} ตัว</small>}
          </span>
          <button
            className="btn small"
            disabled={busy}
            onClick={() =>
              void action(async () => {
                await api("/devices/" + d.id, undefined, "DELETE");
                notify(d.kind === "share" ? "ยกเลิกลิงก์แชร์แล้ว" : "ยกเลิกสิทธิ์อุปกรณ์แล้ว");
              })
            }
          >
            ยกเลิกสิทธิ์
          </button>
        </div>
      ))}
      <small className="muted">
        ติดตั้งเป็นแอป: iPhone Safari → แชร์ → เพิ่มไปยังหน้าจอโฮม · Android/Windows Chrome → ติดตั้งแอป (ต้องใช้ HTTPS)
      </small>
    </div>
  );
}
