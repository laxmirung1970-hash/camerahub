import { useCallback, useEffect, useRef, useState } from "react";
import { LogOut, RefreshCw, Search, Settings } from "lucide-react";
import type { Bootstrap, Camera, System } from "./api";
import { api, onUnauthorized, tokenStore } from "./api";
import { Auth } from "./Auth";
import { CameraDetail } from "./CameraDetail";
import type { CameraEdit } from "./CameraEditor";
import { CameraPicker } from "./CameraPicker";
import { Clock } from "./Clock";
import { chooseStep, displayed, focusCamera, initialDisplay, reordered, toggleLocked } from "./displayState";
import type { DisplayState } from "./displayState";
import { useMediaQuery } from "./hooks";
import { Monitor } from "./Monitor";
import { PAGE_SIZE, groupedOrder } from "./monitoring";
import { SettingsModal } from "./settings/SettingsModal";
import { Logo, Spinner, Toast } from "./ui";
import type { ToastMessage } from "./ui";
import { useRemoteNavigation } from "./useRemoteNavigation";

export default function App() {
  useRemoteNavigation();
  const wide = useMediaQuery("(min-width: 1000px)");
  const [boot, setBoot] = useState<Bootstrap>();
  const [loadError, setLoadError] = useState("");
  const [cams, setCams] = useState<Camera[]>([]);
  const [display, setDisplay] = useState<DisplayState>(initialDisplay);
  const [editingName, setEditingName] = useState(false);
  const [ordering, setOrdering] = useState(false);
  const [group, setGroup] = useState("");
  const [search, setSearch] = useState("");
  const [audible, setAudible] = useState("");
  const [detail, setDetail] = useState<string>();
  const [modal, setModal] = useState("");
  const [toast, setToast] = useState<ToastMessage>();
  const [syncing, setSyncing] = useState(false);
  const [sys, setSys] = useState<System>();
  const [offline, setOffline] = useState(false);
  const [listOpen, setListOpen] = useState(false);
  const wasOffline = useRef(false);
  const notify = useCallback((text: string, kind: "ok" | "error" = "ok") => setToast({ text, kind }), []);
  const refresh = useCallback(async () => {
    try {
      setCams(await api<Camera[]>("/cameras"));
      setOffline(false);
      wasOffline.current = false;
    } catch (e) {
      setOffline(true);
      if (!wasOffline.current) notify((e as Error).message, "error");
      wasOffline.current = true;
    }
  }, [notify]);
  const load = useCallback(() => {
    api<Bootstrap>("/bootstrap")
      .then(setBoot)
      .catch(() => setLoadError("เชื่อมเครื่องกลางไม่ได้ ตรวจว่าเปิดระบบไว้แล้ว"));
  }, []);
  useEffect(load, [load]);
  useEffect(() => {
    onUnauthorized(() => {
      setBoot((b) => b && { ...b, authenticated: false });
      setDetail(undefined);
      setModal("");
    });
  }, []);
  const authenticated = boot?.authenticated ?? false,
    owner = boot?.kind === "owner";
  useEffect(() => {
    if (!authenticated) return;
    void refresh();
    const t = setInterval(() => void refresh(), 15000);
    return () => clearInterval(t);
  }, [authenticated, refresh]);
  useEffect(() => {
    if (!authenticated || !owner) return;
    const poll = () => void api<System>("/system").then(setSys).catch(() => {});
    poll();
    const t = setInterval(poll, 30000);
    return () => clearInterval(t);
  }, [authenticated, owner]);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(undefined), 5000);
    return () => clearTimeout(t);
  }, [toast]);
  if (!boot)
    return (
      <main className="auth">
        <div className="auth-card center">
          <Logo />
          {loadError ? (
            <>
              <p>{loadError}</p>
              <button className="btn" onClick={load}>
                ลองใหม่
              </button>
            </>
          ) : (
            <Spinner size={24} />
          )}
        </div>
      </main>
    );
  if (!boot.authenticated) return <Auth boot={boot} onDone={load} />;

  const groups = [...new Set(cams.map((c) => c.group))].sort();
  const q = search.trim().toLowerCase();
  // Pages of four never mix groups, so the list is shown in group order (owner order inside each group).
  const filtered = groupedOrder(cams.filter((c) => (!group || c.group === group) && (!q || `${c.name} ${c.model} ${c.group}`.toLowerCase().includes(q))));
  const shown = displayed(filtered, display);
  const current = cams.find((c) => c.id === detail);
  const changeDisplay = (next: DisplayState) => {
    setDisplay(next);
    setAudible("");
  };
  const toggle = (id: string) => {
    const chosen = display.mode === "lock4" ? shown : [];
    if (!chosen.some((c) => c.id === id) && chosen.length >= PAGE_SIZE) {
      notify(`เลือกได้ ${PAGE_SIZE} ภาพ เอาเครื่องหมายถูกออกจากกล้องเดิมก่อน`, "error");
      return;
    }
    changeDisplay(toggleLocked(filtered, display, id));
  };
  const rename = async (c: Camera, name: string) => {
    await api(`/cameras/${c.id}`, { name }, "PATCH");
    setCams((prev) => prev.map((x) => (x.id === c.id ? { ...x, name } : x)));
  };
  const edit = async (c: Camera, change: CameraEdit) => {
    await api(`/cameras/${c.id}`, change, "PATCH");
    setCams((prev) => prev.map((x) => (x.id === c.id ? { ...x, ...change } : x)));
    notify(change.group && change.group !== c.group ? `ย้าย ${change.name} ไปกลุ่ม ${change.group} แล้ว` : `บันทึก ${change.name} แล้ว`);
  };
  const moveTo = async (id: string, index: number) => {
    if (ordering) return;
    const next = reordered(cams, filtered, id, index);
    if (next.every((c, i) => c.id === cams[i]?.id)) return;
    setOrdering(true);
    try {
      setCams(await api<Camera[]>("/cameras/order", { ids: next.map((c) => c.id) }, "PUT"));
    } catch (e) {
      notify((e as Error).message, "error");
    } finally {
      setOrdering(false);
    }
  };
  const discover = async () => {
    setSyncing(true);
    try {
      const r = await api<{ message: string }>("/discover", {});
      notify(r.message);
      await refresh();
    } catch (e) {
      notify((e as Error).message, "error");
    } finally {
      setSyncing(false);
    }
  };
  const logout = () =>
    api("/logout", {})
      .catch(() => {})
      .finally(() => {
        tokenStore.clear();
        setCams([]);
        setBoot((b) => b && { ...b, authenticated: false });
      });
  const step = (n: number) => {
    if (!current) return;
    const i = filtered.findIndex((c) => c.id === current.id);
    setDetail(filtered[(i + n + filtered.length) % filtered.length]?.id);
  };
  const gridActive = wide || !detail;
  return (
    <div className={`app${detail ? " has-detail" : ""}`}>
      <header className="topbar">
        <Logo />
        <span className={`badge${boot.mode === "live" ? " live" : ""}`}>{boot.mode === "demo" ? "สาธิต" : "กล้องจริง"}</span>
        {offline && <span className="badge warn">ขาดการเชื่อมต่อเครื่องกลาง</span>}
        {sys && !sys.bridge && <span className="badge warn">ระบบวิดีโอไม่พร้อม</span>}
        <span className="spacer" />
        <Clock />
        {owner && (
          <button className="icon-btn" aria-label="ตั้งค่า" onClick={() => setModal("connections")}>
            <Settings size={20} />
          </button>
        )}
        <button className="icon-btn" aria-label={`ออกจากระบบ ${boot.name}`} title={boot.name} onClick={() => void logout()}>
          <LogOut size={19} />
        </button>
      </header>
      <div className="workspace">
        <Monitor
          cameras={filtered}
          state={display}
          onChange={changeDisplay}
          webRTC={boot.webRTC}
          active={gridActive}
          tourPaused={Boolean(detail || modal || editingName || ordering)}
          audibleId={audible}
          detailId={detail}
          onAudio={(id) => setAudible(audible === id ? "" : id)}
          onOpen={(c) => setDetail(c.id)}
          onRename={owner ? rename : undefined}
          onEditing={setEditingName}
        />
        {current ? (
          <CameraDetail key={current.id} camera={current} boot={boot} onClose={() => setDetail(undefined)} onStep={step} notify={notify} refresh={refresh} />
        ) : (
          <aside className={`rail${listOpen ? " open" : ""}`} aria-label="รายชื่อกล้อง">
            <button className="rail-toggle" aria-expanded={listOpen} onClick={() => setListOpen(!listOpen)}>
              รายชื่อกล้อง ({filtered.length})
            </button>
            <div className="rail-body">
              <div className="row">
                <label className="search">
                  <Search size={16} />
                  <input aria-label="ค้นหากล้อง" placeholder="ค้นหาชื่อกล้อง หรือห้อง" value={search} onChange={(e) => setSearch(e.target.value)} />
                </label>
                {groups.length > 1 && (
                  <select aria-label="กรองกลุ่ม" value={group} onChange={(e) => setGroup(e.target.value)}>
                    <option value="">ทุกกลุ่ม</option>
                    {groups.map((g) => (
                      <option key={g}>{g}</option>
                    ))}
                  </select>
                )}
                {owner && (
                  <button className="icon-btn" aria-label="ค้นหากล้องใหม่" disabled={syncing} onClick={() => void discover()}>
                    <RefreshCw size={16} className={syncing ? "spin" : ""} />
                  </button>
                )}
              </div>
              <CameraPicker
                cameras={filtered}
                groups={groups}
                selected={display.mode === "lock4" ? shown.map((c) => c.id) : []}
                showing={shown.map((c) => c.id)}
                owner={owner}
                ordering={ordering}
                bridgeReady={!owner || sys?.bridge === true}
                onShowPage={(p) => changeDisplay(chooseStep(filtered, { ...display, mode: "lock4", lockedIds: null }, p))}
                onToggle={toggle}
                onFocus={(c) => changeDisplay(focusCamera(filtered, display, c.id))}
                onEdit={owner ? edit : undefined}
                onEditing={setEditingName}
                onMoveTo={(id, i) => void moveTo(id, i)}
              />
              <small className="muted">{owner ? "กดดินสอเพื่อแก้ชื่อหรือย้ายกลุ่ม · กล้องกลุ่มเดียวกันอยู่หน้าเดียวกัน · ติ๊กเลือกล็อก 4 ภาพ" : "ติ๊กเลือกล็อก 4 ภาพ"}</small>
            </div>
          </aside>
        )}
      </div>
      {modal && <SettingsModal tab={modal} setTab={setModal} boot={boot} cameras={cams} onClose={() => setModal("")} notify={notify} refresh={refresh} />}
      {toast && <Toast toast={toast} onClose={() => setToast(undefined)} />}
    </div>
  );
}
