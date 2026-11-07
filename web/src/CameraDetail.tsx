import { useEffect, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, RefreshCw, Volume2, VolumeX, X } from "lucide-react";
import type { Bootstrap, Camera } from "./api";
import { api, statusLabel } from "./api";
import { cameraModel } from "./cameraPresentation";
import { Player } from "./Player";
import { PTZPad } from "./PTZPad";
import { Spinner } from "./ui";

export function CameraDetail({
  camera: c,
  boot,
  onClose,
  onStep,
  notify,
  refresh,
}: {
  camera: Camera;
  boot: Bootstrap;
  onClose: () => void;
  onStep: (step: number) => void;
  notify: (s: string, kind?: "ok" | "error") => void;
  refresh: () => Promise<void>;
}) {
  const [quality, setQuality] = useState("low");
  const [audio, setAudio] = useState(false);
  const [busy, setBusy] = useState("");
  const [probeMessage, setProbeMessage] = useState("");
  const root = useRef<HTMLElement>(null);
  const owner = boot.kind === "owner",
    live = boot.mode === "live";
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    root.current?.focus();
    return () => prev?.focus();
  }, []);
  const run = async (key: string, fn: () => Promise<void>) => {
    setBusy(key);
    try {
      await fn();
    } catch (e) {
      notify((e as Error).message, "error");
    } finally {
      setBusy("");
    }
  };
  const probe = () =>
    run("probe", async () => {
      const r = await api<Camera>(`/cameras/${c.id}/probe`, {});
      notify(r.status === "online" ? "เปิดสตรีมได้แล้ว เสียงและการหมุนยังต้องตรวจแยก" : "ยังเปิดสตรีมไม่ได้ ตรวจบัญชีและเครือข่าย", r.status === "online" ? "ok" : "error");
      await refresh();
    });
  const probeMotor = () =>
    run("motor", async () => {
      const r = await api<{ message: string }>(`/cameras/${c.id}/ptz/probe`, {});
      setProbeMessage(r.message);
      await refresh();
    });
  const save = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    void run("save", async () => {
      await api(
        `/cameras/${c.id}`,
        {
          name: f.get("name"),
          group: f.get("group"),
          playbackMode: f.get("playbackMode"),
          ...(live ? { audioVerified: f.get("audioVerified") === "on", ptzVerified: f.get("ptzVerified") === "on" } : {}),
          ...(live && c.vendor === "xiaomi" ? { ptzProtocol: f.get("ptzProtocol"), audio: f.get("videoOnly") !== "on" } : {}),
        },
        "PATCH",
      );
      await refresh();
      notify("บันทึกกล้องแล้ว");
    });
  };
  const [confirmHide, setConfirmHide] = useState(false);
  const hide = () =>
    run("hide", async () => {
      await api(`/cameras/${c.id}`, { hidden: true }, "PATCH");
      notify(`ถอด ${c.name} ออกจากรายการแล้ว นำกลับได้ที่ ตั้งค่า › เชื่อมกล้อง`);
      onClose();
      await refresh();
    });
  return (
    <aside
      ref={root}
      className="detail"
      role="dialog"
      aria-label={`รายละเอียด ${c.name}`}
      tabIndex={-1}
      onKeyDown={(e) => {
        if (e.key === "Escape" && !(e.target as HTMLElement).closest("input, select, textarea")) {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <header className="detail-head">
        <div>
          <h2>{c.name}</h2>
          <small className="muted">
            {cameraModel(c)} · {c.group}
          </small>
        </div>
        <button className="icon-btn" aria-label="ปิดรายละเอียด" onClick={onClose}>
          <X size={18} />
        </button>
      </header>
      <div className="detail-video">
        <Player camera={c} quality={quality} audible={audio} webRTC={boot.webRTC} />
      </div>
      <div className="row">
        <button className="icon-btn" onClick={() => onStep(-1)} aria-label="กล้องก่อนหน้า">
          <ChevronLeft size={20} />
        </button>
        <button className="btn small" onClick={() => setAudio(!audio)} disabled={!c.audio} aria-pressed={audio}>
          {audio ? <Volume2 size={16} /> : <VolumeX size={16} />}
          เสียง
        </button>
        <select aria-label="คุณภาพภาพ" value={quality} onChange={(e) => setQuality(e.target.value)}>
          <option value="low">ประหยัด</option>
          <option value="high">ละเอียดสูง</option>
        </select>
        <span className="status">
          <i className={`dot ${c.status}`} />
          {statusLabel(c.status)}
        </span>
        <button className="icon-btn" onClick={() => onStep(1)} aria-label="กล้องถัดไป">
          <ChevronRight size={20} />
        </button>
      </div>
      {c.ptz ? (
        <>
          <PTZPad camera={c} onError={(m) => notify(m, "error")} />
          <small className="muted center">
            {c.verification === "demo" ? "จำลองคำสั่ง ภาพตัวอย่างไม่หมุนตาม" : "กดค้างเพื่อหมุน หยุดเมื่อปล่อย"}
          </small>
        </>
      ) : (
        <div className="row wrap">
          <small className="muted">ยังไม่เปิดใช้การหมุน</small>
          {owner && c.vendor === "xiaomi" && live && (
            <button className="btn small" disabled={busy === "motor"} onClick={() => void probeMotor()}>
              {busy === "motor" ? <Spinner size={14} /> : null}
              ตรวจระบบหมุน
            </button>
          )}
        </div>
      )}
      {probeMessage && <p className="muted" role="status">{probeMessage}</p>}
      <div className="row wrap">
        <small className="muted">
          {boot.mode === "demo"
            ? "ภาพและคำสั่งทั้งหมดเป็นการสาธิต"
            : `ภาพ: ${c.verification === "stream_only" ? "ผ่าน" : "ยังไม่ตรวจ"} · เสียง: ${c.audioVerified ? "ยืนยันแล้ว" : "ยังไม่ยืนยัน"} · หมุน: ${c.ptzVerified ? "ยืนยันแล้ว" : "ยังไม่ยืนยัน"}`}
        </small>
        {owner && (
          <button className="btn small" disabled={busy === "probe"} onClick={() => void probe()}>
            {busy === "probe" ? <Spinner size={14} /> : <RefreshCw size={14} />}
            ตรวจภาพ
          </button>
        )}
      </div>
      {owner && (
        <details className="detail-edit">
          <summary>ชื่อ กลุ่ม และการเล่นวิดีโอ</summary>
          <form className="form" onSubmit={save}>
            <label>
              ชื่อกล้อง
              <input name="name" defaultValue={c.name} required maxLength={60} />
            </label>
            <label>
              กลุ่ม / ห้อง
              <input name="group" defaultValue={c.group} />
            </label>
            <label>
              การเล่นวิดีโอ
              <select name="playbackMode" defaultValue={c.playbackMode || "native"}>
                <option value="native">ส่งต่อภาพเดิม · ใช้เครื่องน้อย</option>
                <option value="h264">แปลงเป็น H.264 · เมื่ออุปกรณ์เปิดภาพเดิมไม่ได้</option>
              </select>
            </label>
            {live && c.vendor === "xiaomi" && (
              <label>
                การหมุน Xiaomi
                <select name="ptzProtocol" defaultValue={c.ptz ? "miss-step" : "none"}>
                  <option value="none">ยังไม่เปิดใช้</option>
                  <option value="miss-step">MISS ทีละก้าว · ต้องตรวจรุ่นจริง</option>
                </select>
              </label>
            )}
            {live && c.vendor === "xiaomi" && (
              <label className="check">
                <input type="checkbox" name="videoOnly" defaultChecked={!c.audio} />
                กล้องนี้ไม่ส่งเสียง · เปิดเฉพาะภาพ (ใช้เมื่อปิดไมค์ไว้แล้วภาพไม่ขึ้น)
              </label>
            )}
            {live && (
              <>
                <label className="check">
                  <input type="checkbox" name="audioVerified" defaultChecked={c.audioVerified} />
                  ยืนยันฟังเสียงจากกล้องจริงแล้ว
                </label>
                <label className="check">
                  <input type="checkbox" name="ptzVerified" disabled={!c.ptz} defaultChecked={c.ptzVerified} />
                  ยืนยันทิศทางหมุนกับกล้องจริงแล้ว
                </label>
              </>
            )}
            {boot.mode === "demo" && (
              <label>
                จำลองสถานะกล้อง
                <select
                  value={c.status}
                  onChange={(e) =>
                    void run("scenario", async () => {
                      await api(`/demo/cameras/${c.id}/scenario`, { status: e.target.value });
                      await refresh();
                    })
                  }
                >
                  <option value="online">พร้อมดู</option>
                  <option value="offline">ออฟไลน์</option>
                  <option value="auth_expired">บัญชีหมดอายุ</option>
                </select>
              </label>
            )}
            <small className="muted">
              IP {c.host} · รหัส {c.ref}
            </small>
            <button className="btn primary small" disabled={busy === "save"}>
              บันทึก
            </button>
          </form>
          <div className="row wrap">
            {confirmHide ? (
              <>
                <small className="muted">ซ่อนจากหน้ารวม ทีวี และลิงก์แชร์ ไม่ลบข้อมูล นำกลับได้ภายหลัง</small>
                <button className="btn small" disabled={busy === "hide"} onClick={() => void hide()}>
                  {busy === "hide" ? <Spinner size={14} /> : null}
                  ยืนยันถอดออก
                </button>
                <button className="btn small" onClick={() => setConfirmHide(false)}>
                  ยกเลิก
                </button>
              </>
            ) : (
              <button className="btn small" onClick={() => setConfirmHide(true)}>
                ถอดกล้องนี้ออกจากรายการ
              </button>
            )}
          </div>
        </details>
      )}
    </aside>
  );
}
