import { useState } from "react";
import { Copy, Download } from "lucide-react";
import { api, apiBlob } from "../api";
import type { TabProps } from "./SettingsModal";

const SERVE = "tailscale serve --bg http://127.0.0.1:8080";

export function SystemTab({ boot, data, busy, action, notify, refresh }: TabProps) {
  const [pass, setPass] = useState("");
  const [file, setFile] = useState<File>();
  const [confirm, setConfirm] = useState("");
  const system = data.system;
  const exportBackup = () =>
    void action(async () => {
      const blob = await apiBlob("/backup", { password: pass });
      const u = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = u;
      a.download = `camerahub-${boot.mode}.cambackup`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(u), 1000);
      setPass("");
      notify("ดาวน์โหลดไฟล์สำรองแล้ว เก็บรหัสแยกจากไฟล์");
    });
  const restore = () =>
    void action(async () => {
      if (!file) throw new Error("เลือกไฟล์สำรองก่อน");
      const bytes = new Uint8Array(await file.arrayBuffer());
      if (bytes.length > 1200000) throw new Error("ไฟล์ใหญ่เกินไป");
      let binary = "";
      for (const b of bytes) binary += String.fromCharCode(b);
      await api("/backup/restore", { password: pass, file: btoa(binary), confirm });
      await refresh();
      setPass("");
      setConfirm("");
      notify("กู้คืนแล้ว ให้รีสตาร์ตตัวกลางวิดีโอก่อนดูภาพ");
    });
  return (
    <div className="stack">
      <section className="card">
        <strong>{system?.bridge ? "ระบบกลางพร้อมใช้งาน" : "กำลังตรวจตัวกลางวิดีโอ"}</strong>
        <br />
        <small className="muted">
          {boot.mode === "demo" ? "สาธิต" : "ใช้งานจริง"} · v{boot.version} · หน่วยความจำแอป {system?.appMemoryMB ?? "—"} MB · {system?.architecture || "—"}
        </small>
      </section>
      <section className="stack">
        <h3>ดูนอกบ้านด้วย Tailscale</h3>
        <div className="row">
          <code>{SERVE}</code>
          <button className="icon-btn" aria-label="คัดลอกคำสั่ง" onClick={() => void navigator.clipboard?.writeText(SERVE).then(() => notify("คัดลอกแล้ว"))}>
            <Copy size={16} />
          </button>
        </div>
        <form
          className="row"
          onSubmit={(e) => {
            e.preventDefault();
            const f = new FormData(e.currentTarget);
            void action(async () => {
              await api("/system/remote", { url: f.get("url") });
              notify("บันทึกที่อยู่แล้ว");
            });
          }}
        >
          <input key={system?.remoteURL} name="url" type="url" defaultValue={system?.remoteURL} placeholder="https://nas.your-tailnet.ts.net" aria-label="ที่อยู่ HTTPS" />
          <button className="btn" disabled={busy}>
            บันทึก
          </button>
        </form>
      </section>
      <section className="stack">
        <h3>สำรองการตั้งค่าแบบเข้ารหัส</h3>
        <small className="muted">รวมทะเบียนกล้องและข้อมูลเชื่อมต่อ ไม่รวมวิดีโอและรหัสเจ้าของระบบ</small>
        <label>
          รหัสไฟล์สำรอง (อย่างน้อย 12 ตัวอักษร)
          <input type="password" value={pass} onChange={(e) => setPass(e.target.value)} autoComplete="new-password" />
        </label>
        <button className="btn" disabled={busy || pass.length < 12} onClick={exportBackup}>
          <Download size={16} />
          ดาวน์โหลดไฟล์สำรอง
        </button>
        <details>
          <summary>กู้คืนไฟล์สำรอง · แทนที่รายการปัจจุบัน</summary>
          <div className="form">
            <label>
              ไฟล์ .cambackup
              <input type="file" accept=".cambackup" onChange={(e) => setFile(e.target.files?.[0])} />
            </label>
            <label>
              พิมพ์ RESTORE เพื่อยืนยัน
              <input value={confirm} onChange={(e) => setConfirm(e.target.value)} placeholder="RESTORE" />
            </label>
            <button className="btn danger" disabled={busy || !file || confirm !== "RESTORE" || pass.length < 12} onClick={restore}>
              แทนที่ด้วยข้อมูลสำรอง
            </button>
          </div>
        </details>
      </section>
      <section className="stack">
        <h3>เหตุการณ์ล่าสุด</h3>
        <ul className="events">
          {system?.events.map((e) => (
            <li key={e.id}>
              <span>{e.message}</span>
              <small className="muted">{new Date(e.time).toLocaleString("th-TH", { timeZone: "Asia/Bangkok" })}</small>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
