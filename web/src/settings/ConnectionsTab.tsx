import { useEffect, useState } from "react";
import { Plus, RefreshCw } from "lucide-react";
import type { Camera } from "../api";
import { api } from "../api";
import { Notice, Spinner } from "../ui";
import type { TabProps } from "./SettingsModal";

type Status = { kind: "busy" | "success" | "error" | "info"; message: string };
const Result = ({ status }: { status?: Status }) =>
  status ? <Notice warning={status.kind === "error"}>{status.message}</Notice> : null;

export function ConnectionsTab({ boot, data, busy, action, notify, refresh }: TabProps) {
  const [tapo, setTapo] = useState<Status>();
  const [xiaomi, setXiaomi] = useState<Status>();
  const [discovery, setDiscovery] = useState<{ vendor: string } & Status>();
  const [xstep, setXstep] = useState("login");
  const [captcha, setCaptcha] = useState("");
  const [destination, setDestination] = useState("");
  const [accounts, setAccounts] = useState<string[]>([]);
  const [account, setAccount] = useState("");
  const [hidden, setHidden] = useState<Camera[]>([]);
  const live = boot.mode === "live";
  useEffect(() => {
    if (live) void api<string[]>("/xiaomi/accounts").then(setAccounts).catch(() => {});
  }, [live]);
  useEffect(() => {
    void api<Camera[]>("/cameras?hidden=1").then(setHidden).catch(() => {});
  }, []);
  const restore = (c: Camera) =>
    void action(async () => {
      await api(`/cameras/${c.id}`, { hidden: false }, "PATCH");
      setHidden((list) => list.filter((x) => x.id !== c.id));
      await refresh();
      notify(`นำ ${c.name} กลับเข้ารายการแล้ว`);
    });
  const withTimeout = async <T,>(ms: number, fn: (signal: AbortSignal) => Promise<T>) => {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), ms);
    try {
      return await fn(controller.signal);
    } finally {
      clearTimeout(timer);
    }
  };
  const discover = (vendor = "") =>
    void action(async () => {
      setDiscovery({ vendor, kind: "busy", message: "กำลังค้นหากล้อง กรุณารอสักครู่…" });
      try {
        const r = await withTimeout(65000, (signal) =>
          api<{ message: string }>("/discover" + (vendor ? `?vendor=${vendor}` : ""), {}, undefined, signal),
        );
        setDiscovery({ vendor, kind: "success", message: r.message });
      } catch (e) {
        setDiscovery({
          vendor,
          kind: "error",
          message: (e as Error).name === "AbortError" ? "ค้นหานานเกินไป ตรวจสถานะการเชื่อมต่อด้านล่าง" : (e as Error).message,
        });
      } finally {
        await refresh();
      }
    });
  const saveConnection = (vendor: string) => (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    void action(async () => {
      await api("/connections", {
        vendor,
        name: vendor === "tapo" ? "Tapo ในบ้าน" : "Xiaomi Home",
        account: f.get("account") || "",
        region: f.get("region") || "",
        username: f.get("username") || "",
        password: f.get("password") || "",
        enabled: f.get("enabled") === "on",
      });
      notify("บันทึกแล้ว กดค้นหากล้องเพื่อเริ่มเชื่อมต่อ");
    });
  };
  const addTapo = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    void action(async () => {
      setTapo({ kind: "busy", message: "กำลังติดต่อกล้องและตรวจบัญชี รอได้ประมาณ 30 วินาที" });
      try {
        const camera = await withTimeout(35000, (signal) =>
          api<Camera>(
            "/cameras/tapo",
            { host: f.get("host"), name: f.get("name"), username: f.get("username"), password: f.get("password") },
            "POST",
            signal,
          ),
        );
        await refresh();
        setTapo({ kind: "success", message: `เพิ่ม ${camera.name} แล้ว เลือกกล้องและกดตรวจภาพในรายละเอียด` });
      } catch (e) {
        const message = (e as Error).name === "AbortError" ? "รอการเชื่อมต่อนานเกินไป ตรวจเครื่องกลางและเครือข่าย" : (e as Error).message;
        setTapo({ kind: "error", message });
        throw new Error(message);
      }
    });
  };
  const authXiaomi = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    void action(async () => {
      setXiaomi({ kind: "busy", message: "กำลังเชื่อมบัญชี Xiaomi…" });
      try {
        const out = await withTimeout(35000, (signal) =>
          api<{ step: string; captcha?: string; destination?: string; accounts?: string[] }>(
            "/xiaomi/auth",
            xstep === "login"
              ? { username: f.get("username"), password: f.get("password") }
              : xstep === "captcha"
                ? { captcha: f.get("captcha") }
                : { verify: f.get("verify") },
            undefined,
            signal,
          ),
        );
        if (out.step === "done" && !out.accounts?.length) throw new Error("ยังไม่มีบัญชีที่เชื่อมสำเร็จ กรุณาเริ่มใหม่");
        if (!["done", "captcha", "verify"].includes(out.step)) throw new Error("ขั้นตอนยืนยันที่แอปยังไม่รองรับ กรุณาเริ่มใหม่");
        setXstep(out.step);
        setCaptcha(out.captcha || "");
        setDestination(out.destination || "");
        if (out.accounts) setAccounts(out.accounts);
        setXiaomi({
          kind: out.step === "done" ? "success" : "info",
          message:
            out.step === "done"
              ? "เชื่อมบัญชีสำเร็จ เลือกบัญชีและ Region ด้านล่างแล้วบันทึกการซิงก์"
              : out.step === "captcha"
                ? "กรอกตัวอักษรจากภาพแล้วกดยืนยัน"
                : "Xiaomi ส่งรหัสยืนยันแล้ว ตรวจ SMS หรืออีเมล",
        });
      } catch (e) {
        setXiaomi({
          kind: "error",
          message: (e as Error).name === "AbortError" ? "รอนานเกินไป ตรวจอินเทอร์เน็ตแล้วลองอีกครั้ง" : (e as Error).message,
        });
      }
    });
  };
  const lastXiaomiError = data.system?.events.find((e) => e.kind === "xiaomi_auth");
  return (
    <div className="stack">
      {!live ? (
        <>
          <Notice>โหมดสาธิตไม่รับบัญชีจริง และไม่ค้นหากล้องในเครือข่าย</Notice>
          <button
            className="btn"
            disabled={busy}
            onClick={() =>
              void action(async () => {
                await api("/demo/cameras", {});
                await refresh();
                notify("เพิ่มกล้องจำลองแล้ว");
              })
            }
          >
            <Plus size={16} />
            เพิ่มกล้องจำลอง
          </button>
        </>
      ) : (
        <>
          <section className="stack">
            <h3>Tapo · เพิ่มด้วย IP กล้อง</h3>
            <form className="form" onSubmit={addTapo}>
              <label>
                IP กล้อง
                <input name="host" required placeholder="192.168.1.50" />
              </label>
              <label>
                ชื่อกล้อง
                <input name="name" placeholder="เช่น ประตูหน้าบ้าน" />
              </label>
              <label>
                ชื่อผู้ใช้ Camera Account
                <input name="username" required autoComplete="off" />
              </label>
              <label>
                รหัสผ่านกล้อง
                <input name="password" type="password" required autoComplete="new-password" />
              </label>
              <button className="btn primary" disabled={busy}>
                {busy && tapo?.kind === "busy" ? <Spinner size={16} /> : null}
                เพิ่มกล้องนี้
              </button>
              <Result status={tapo} />
            </form>
            <details>
              <summary>ค้นหา Tapo อัตโนมัติด้วยบัญชีเดียวกัน</summary>
              <form className="form" onSubmit={saveConnection("tapo")}>
                <label>
                  ชื่อผู้ใช้ Camera Account
                  <input name="username" required autoComplete="off" />
                </label>
                <label>
                  รหัสผ่านกล้อง
                  <input name="password" type="password" required autoComplete="new-password" />
                </label>
                <label className="check">
                  <input name="enabled" type="checkbox" defaultChecked />
                  เปิดการค้นหา ONVIF ทุก 2 นาที
                </label>
                <button className="btn" disabled={busy}>
                  บันทึกบัญชีกล้อง
                </button>
              </form>
            </details>
          </section>
          <section className="stack">
            <h3>Xiaomi Home</h3>
            {accounts.length > 0 && <Notice>เชื่อมบัญชี Xiaomi แล้ว {accounts.length} บัญชี เลือกบัญชีและ Region แล้วบันทึกการซิงก์</Notice>}
            {xstep !== "done" && (
              <details open={accounts.length === 0}>
                <summary>{accounts.length ? "เชื่อมบัญชีเพิ่ม" : "เข้าสู่บัญชี Xiaomi"}</summary>
                <form className="form" onSubmit={authXiaomi}>
                  {xstep === "login" ? (
                    <>
                      <label>
                        บัญชี Xiaomi / อีเมล
                        <input name="username" required autoComplete="off" />
                      </label>
                      <label>
                        รหัสผ่าน Xiaomi
                        <input name="password" required type="password" autoComplete="new-password" />
                      </label>
                    </>
                  ) : xstep === "captcha" ? (
                    <>
                      <img className="captcha" alt="รหัสภาพยืนยัน Xiaomi" src={"data:image/jpeg;base64," + captcha} />
                      <label>
                        ตัวอักษรในภาพ
                        <input name="captcha" required autoComplete="off" />
                      </label>
                    </>
                  ) : (
                    <label>
                      รหัสยืนยันที่ส่งไป {destination}
                      <input name="verify" required autoComplete="one-time-code" />
                    </label>
                  )}
                  <div className="row">
                    <button className="btn" disabled={busy}>
                      {busy && xiaomi?.kind === "busy" ? <Spinner size={15} /> : null}
                      {xstep === "login" ? "เชื่อมบัญชี" : "ยืนยันรหัส"}
                    </button>
                    {xstep !== "login" && (
                      <button
                        type="button"
                        className="text-btn"
                        onClick={() => {
                          setXstep("login");
                          setXiaomi(undefined);
                        }}
                      >
                        เริ่มใหม่
                      </button>
                    )}
                  </div>
                </form>
              </details>
            )}
            <Result status={xiaomi} />
            {!xiaomi && accounts.length === 0 && lastXiaomiError && (
              <Notice warning>
                ผลการเชื่อมครั้งล่าสุด · {new Date(lastXiaomiError.time).toLocaleTimeString("th-TH", { timeZone: "Asia/Bangkok" })}
                <br />
                {lastXiaomiError.message}
              </Notice>
            )}
            <form className="form" onSubmit={saveConnection("xiaomi")}>
              <label>
                รหัสบัญชี Xiaomi
                <select name="account" required value={accounts.length === 1 ? accounts[0] : account} onChange={(e) => setAccount(e.target.value)}>
                  <option value="">เลือกบัญชีที่เชื่อมแล้ว</option>
                  {accounts.map((id) => (
                    <option key={id}>{id}</option>
                  ))}
                </select>
              </label>
              <label>
                Region เดียวกับ Xiaomi Home
                <select name="region" defaultValue="sg">
                  <option value="sg">Singapore / ไทย (sg)</option>
                  <option value="cn">Mainland China (cn)</option>
                  <option value="de">Europe (de)</option>
                  <option value="us">United States (us)</option>
                  <option value="i2">India (i2)</option>
                  <option value="ru">Russia (ru)</option>
                </select>
              </label>
              <label className="check">
                <input name="enabled" type="checkbox" defaultChecked />
                เปิดการซิงก์กล้องทุก 5 นาที
              </label>
              <button className="btn primary" disabled={busy || accounts.length === 0}>
                บันทึกการซิงก์
              </button>
            </form>
            {data.connections.some((c) => c.vendor === "xiaomi" && c.enabled) && (
              <button className="btn" disabled={busy} onClick={() => discover("xiaomi")}>
                <RefreshCw size={16} className={discovery?.kind === "busy" && discovery.vendor === "xiaomi" ? "spin" : ""} />
                ค้นหากล้อง Xiaomi
              </button>
            )}
            {discovery?.vendor === "xiaomi" && <Result status={discovery} />}
          </section>
        </>
      )}
      {data.connections.length > 0 && (
        <section className="stack">
          <h3>การเชื่อมต่อที่บันทึก</h3>
          {data.connections.map((c) => (
            <div className="row between" key={c.id}>
              <span>
                <strong>{c.name}</strong>
                <br />
                <small className="muted">
                  {c.message || "ยังไม่ซิงก์"} · {c.region || "ONVIF"}
                </small>
              </span>
              <button
                className={`btn small${c.enabled ? "" : " muted"}`}
                disabled={busy}
                onClick={() => void action(() => api("/connections/" + c.id + "/toggle", { enabled: !c.enabled }))}
              >
                {c.enabled ? "เปิดอยู่" : "หยุดอยู่"}
              </button>
            </div>
          ))}
        </section>
      )}
      {hidden.length > 0 && (
        <section className="stack">
          <h3>กล้องที่ถอดออกจากรายการ</h3>
          {hidden.map((c) => (
            <div className="row between" key={c.id}>
              <span>
                <strong>{c.name}</strong>
                <br />
                <small className="muted">ไม่แสดงในหน้ารวม ทีวี และลิงก์แชร์ · การค้นหากล้องจะไม่ดึงกลับมาเอง</small>
              </span>
              <button className="btn small" disabled={busy} onClick={() => restore(c)}>
                นำกลับ
              </button>
            </div>
          ))}
        </section>
      )}
      {live && (
        <>
          <button className="btn" disabled={busy} onClick={() => discover()}>
            <RefreshCw size={16} className={discovery?.kind === "busy" && !discovery.vendor ? "spin" : ""} />
            ค้นหากล้องทุกระบบ
          </button>
          {discovery && !discovery.vendor && <Result status={discovery} />}
        </>
      )}
    </div>
  );
}
