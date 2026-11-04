import { useCallback, useEffect, useRef, useState } from "react";
import { Maximize2, Minimize2, Volume2, VolumeX } from "lucide-react";
import type { Bootstrap, Camera } from "../api";
import { api, onUnauthorized, tokenStore } from "../api";
import { Clock } from "../Clock";
import { Player } from "../Player";
import { Logo, Spinner } from "../ui";
import { takeSecretFromHash } from "./shareLink";

const TITLE = "ห้องอาม่า";

/**
 * View-only family app: the share token arrives in the URL fragment, the
 * backend already limits the camera list to that link, so there is nothing to
 * configure here. No PTZ, no settings, no search.
 */
export default function LiteApp() {
  const [boot, setBoot] = useState<Bootstrap>();
  const [loadError, setLoadError] = useState("");
  const [cams, setCams] = useState<Camera[]>([]);
  const [offline, setOffline] = useState(false);
  const [audible, setAudible] = useState(false);
  const [focus, setFocus] = useState("");
  const loaded = useRef(false);
  const refresh = useCallback(async () => {
    try {
      setCams(await api<Camera[]>("/cameras"));
      setOffline(false);
    } catch {
      setOffline(true);
    }
  }, []);
  const load = useCallback(() => {
    api<Bootstrap>("/bootstrap")
      .then(setBoot)
      .catch(() => setLoadError("เชื่อมเครื่องกลางไม่ได้ ลองใหม่อีกครั้ง"));
  }, []);
  useEffect(() => {
    if (loaded.current) return;
    loaded.current = true;
    const fragment = takeSecretFromHash(location.hash);
    if (fragment) {
      tokenStore.set(fragment);
      history.replaceState(null, "", location.pathname + location.search);
    }
    onUnauthorized(() => setBoot((b) => b && { ...b, authenticated: false }));
    load();
  }, [load]);
  const authenticated = boot?.authenticated ?? false;
  useEffect(() => {
    if (!authenticated) return;
    void refresh();
    const t = setInterval(() => void refresh(), 15000);
    return () => clearInterval(t);
  }, [authenticated, refresh]);
  useEffect(() => {
    document.title = TITLE;
  }, []);

  if (!boot || !boot.authenticated)
    return (
      <main className="auth">
        <div className="auth-card center">
          <Logo />
          {!boot && !loadError ? (
            <Spinner size={24} />
          ) : (
            <>
              <h1>{TITLE}</h1>
              <p>{loadError || "ลิงก์ไม่ถูกต้องหรือถูกยกเลิก ขอลิงก์ใหม่จากเจ้าของ"}</p>
              {loadError && (
                <button className="btn" onClick={load}>
                  ลองใหม่
                </button>
              )}
            </>
          )}
        </div>
      </main>
    );
  const focused = cams.find((c) => c.id === focus);
  const shown = focused ? [focused] : cams;
  return (
    <div className="app lite">
      <header className="topbar">
        <Logo compact />
        <strong>{TITLE}</strong>
        {offline && <span className="badge warn">ขาดการเชื่อมต่อเครื่องกลาง</span>}
        <span className="spacer" />
        <Clock />
        <button
          className="icon-btn"
          aria-label={audible ? "ปิดเสียง" : "เปิดเสียง"}
          aria-pressed={audible}
          onClick={() => setAudible(!audible)}
        >
          {audible ? <Volume2 size={18} /> : <VolumeX size={18} />}
        </button>
      </header>
      <main className={`lite-grid${focused ? " one" : ""}`}>
        {cams.length === 0 && !offline && <p className="muted lite-empty">ยังไม่มีกล้องในลิงก์นี้</p>}
        {shown.map((c) => (
          <article className="tile" key={c.id} data-camera-id={c.id}>
            <div className="tile-video" onClick={() => setFocus(focused ? "" : c.id)}>
              <Player camera={c} quality={focused ? "high" : "low"} audible={audible} />
              <button className="tile-open" aria-label={focused ? `ย่อ ${c.name}` : `ขยาย ${c.name}`} />
            </div>
            <div className="tile-caption">
              <span className="name">{c.name}</span>
              <span className="model">{focused ? <Minimize2 size={16} /> : <Maximize2 size={16} />}</span>
            </div>
          </article>
        ))}
      </main>
    </div>
  );
}
