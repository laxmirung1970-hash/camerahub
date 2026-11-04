import { useState } from "react";
import type { Bootstrap } from "./api";
import { signIn } from "./api";
import { Logo, Spinner } from "./ui";

export function Auth({ boot, onDone }: { boot: Bootstrap; onDone: () => void }) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    const f = new FormData(e.currentTarget);
    try {
      await signIn(
        boot.needsSetup ? "/setup" : "/login",
        boot.needsSetup
          ? { code: f.get("code"), name: f.get("name"), password: f.get("password") }
          : { password: f.get("password") },
      );
      onDone();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <main className="auth">
      <form className="auth-card" onSubmit={submit}>
        <Logo />
        <span className={`badge${boot.mode === "live" ? " live" : ""}`}>
          {boot.mode === "demo" ? "โหมดสาธิต" : "กล้องจริง"}
        </span>
        <h1>{boot.needsSetup ? "ตั้งค่าเจ้าของระบบ" : "เข้าสู่ระบบ"}</h1>
        {boot.needsSetup && (
          <>
            <label>
              รหัสตั้งค่าครั้งแรก
              <input name="code" required autoComplete="off" placeholder="รหัสจาก scripts/Show-SetupCode.ps1" />
            </label>
            <label>
              ชื่อเจ้าของระบบ
              <input name="name" required minLength={2} maxLength={80} autoComplete="username" />
            </label>
          </>
        )}
        <label>
          รหัสผ่าน
          <input
            name="password"
            required
            type="password"
            minLength={boot.needsSetup ? 8 : 1}
            maxLength={256}
            placeholder={boot.needsSetup ? "อย่างน้อย 8 ตัวอักษร" : ""}
            autoComplete={boot.needsSetup ? "new-password" : "current-password"}
          />
        </label>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <button className="btn primary" disabled={busy}>
          {busy && <Spinner size={16} />}
          {boot.needsSetup ? "สร้างพื้นที่ดูกล้อง" : "เข้าสู่ระบบ"}
        </button>
        <small className="muted">รหัสผ่านของแอปนี้ แยกจากบัญชี Tapo และ Xiaomi</small>
      </form>
    </main>
  );
}
