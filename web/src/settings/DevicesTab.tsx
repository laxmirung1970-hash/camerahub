import { Tv } from "lucide-react";
import { api } from "../api";
import { Notice } from "../ui";
import type { TabProps } from "./SettingsModal";

export function DevicesTab({ data, busy, action, notify }: TabProps) {
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
      <h3>อุปกรณ์ที่จับคู่แล้ว</h3>
      {data.devices.length === 0 && <small className="muted">ยังไม่มีทีวีที่จับคู่</small>}
      {data.devices.map((d) => (
        <div className="row between card" key={d.id}>
          <span>
            <Tv size={18} /> <strong>{d.name}</strong>
          </span>
          <button
            className="btn small"
            disabled={busy}
            onClick={() =>
              void action(async () => {
                await api("/devices/" + d.id, undefined, "DELETE");
                notify("ยกเลิกสิทธิ์อุปกรณ์แล้ว");
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
