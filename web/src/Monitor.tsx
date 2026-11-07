import { useEffect, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, Maximize2, Minimize2 } from "lucide-react";
import type { Camera } from "./api";
import { CameraTile } from "./CameraTile";
import { advance, chooseMode, displayed, isSingle, isTour } from "./displayState";
import type { DisplayMode, DisplayState } from "./displayState";
import { usePageVisible } from "./hooks";
import { PAGE_SIZE, clampPage, pageCount } from "./monitoring";
import { readStorage, writeStorage } from "./storage";

const modes: { mode: DisplayMode; label: string }[] = [
  { mode: "tour4", label: "วน 4 ภาพ" },
  { mode: "tour1", label: "วนภาพใหญ่" },
  { mode: "lock4", label: "ล็อก 4 ภาพ" },
  { mode: "focus", label: "โฟกัส 1 ภาพ" },
];
const SECONDS_KEY = "camerahub.monitor.seconds";

export function Monitor({
  cameras,
  state,
  onChange,
  webRTC,
  active,
  tourPaused,
  audibleId,
  detailId,
  onAudio,
  onOpen,
  onRename,
  onEditing,
}: {
  cameras: Camera[];
  state: DisplayState;
  onChange: (state: DisplayState) => void;
  webRTC: boolean;
  active: boolean;
  tourPaused: boolean;
  audibleId: string;
  detailId?: string;
  onAudio: (id: string) => void;
  onOpen: (c: Camera) => void;
  onRename?: (c: Camera, name: string) => Promise<void>;
  onEditing: (editing: boolean) => void;
}) {
  const [seconds, setSeconds] = useState(() => {
    const n = Number(readStorage(SECONDS_KEY));
    return [15, 30, 60].includes(n) ? n : 30;
  });
  const [remaining, setRemaining] = useState(seconds);
  const [expanded, setExpanded] = useState(false);
  const visible = usePageVisible();
  const root = useRef<HTMLDivElement>(null);
  const touch = useRef<{ x: number; y: number } | null>(null);
  const latest = useRef({ cameras, state, onChange });
  latest.current = { cameras, state, onChange };
  const single = isSingle(state.mode),
    rotating = isTour(state.mode);
  const view = displayed(cameras, state);
  const count = single ? cameras.length : pageCount(cameras);
  const current = single
    ? Math.max(0, cameras.findIndex((c) => c.id === view[0]?.id))
    : clampPage(state.page, cameras);
  // Pages follow groups, so the page label can name the group when there is more than one.
  const grouped = new Set(cameras.map((c) => c.group)).size > 1;
  const pageGroup = !single && grouped ? view[0]?.group : "";
  const signature = cameras.map((c) => c.id).join("|");
  const running = rotating && active && !tourPaused && visible && count > 1;
  useEffect(() => {
    const update = () => {
      if (document.fullscreenElement !== root.current) setExpanded(false);
    };
    document.addEventListener("fullscreenchange", update);
    return () => document.removeEventListener("fullscreenchange", update);
  }, []);
  useEffect(() => {
    setRemaining(seconds);
    if (!running) return;
    const due = Date.now() + seconds * 1000;
    const timer = setInterval(() => {
      const left = Math.max(0, Math.ceil((due - Date.now()) / 1000));
      setRemaining(left);
      if (left === 0) {
        const v = latest.current;
        v.onChange(advance(v.cameras, v.state, 1));
      }
    }, 1000);
    return () => clearInterval(timer);
  }, [state.mode, current, signature, seconds, running]);
  const turn = (step: number) => onChange(advance(cameras, state, step));
  const toggleFullscreen = () => {
    if (expanded) {
      setExpanded(false);
      if (document.fullscreenElement === root.current) void document.exitFullscreen().catch(() => {});
    } else {
      setExpanded(true);
      void root.current?.requestFullscreen?.().catch(() => {});
    }
  };
  const open = async (camera: Camera) => {
    if (document.fullscreenElement === root.current) await document.exitFullscreen().catch(() => {});
    setExpanded(false);
    onOpen(camera);
  };
  return (
    <section
      ref={root}
      className={`monitor${single ? " single" : ""}${expanded ? " is-expanded" : ""}`}
      aria-label="หน้าจอดูกล้อง"
      onKeyDown={(e) => {
        if ((e.target as HTMLElement).closest("input, select, textarea")) return;
        if (e.key === "Escape" && expanded) {
          e.stopPropagation();
          toggleFullscreen();
        } else if (e.key === "PageDown" || e.key === "PageUp") {
          e.preventDefault();
          turn(e.key === "PageDown" ? 1 : -1);
        }
      }}
    >
      <div className="monitor-bar">
        <div className="segmented" role="radiogroup" aria-label="โหมดดูกล้อง">
          {modes.map((m) => (
            <button
              key={m.mode}
              role="radio"
              aria-checked={state.mode === m.mode}
              onClick={() => onChange(chooseMode(cameras, state, m.mode))}
            >
              {m.label}
            </button>
          ))}
        </div>
        <div className="pager">
          <button className="icon-btn" onClick={() => turn(-1)} disabled={count < 2} aria-label={single ? "กล้องก่อนหน้า" : "หน้าก่อนหน้า"}>
            <ChevronLeft size={20} />
          </button>
          <span className="pager-label" aria-live="polite">
            {single ? "กล้อง" : "หน้า"} {Math.min(current + 1, count)} / {count}
            {pageGroup && <small className="pager-group"> · {pageGroup}</small>}
          </span>
          <button className="icon-btn" onClick={() => turn(1)} disabled={count < 2} aria-label={single ? "กล้องถัดไป" : "หน้าถัดไป"}>
            <ChevronRight size={20} />
          </button>
          {rotating ? (
            <label className="interval">
              <span role="timer">{running ? `${remaining} วิ` : "พัก"}</span>
              <select
                aria-label="เวลาสลับภาพ"
                value={seconds}
                onChange={(e) => {
                  const n = Number(e.target.value);
                  setSeconds(n);
                  writeStorage(SECONDS_KEY, String(n));
                }}
              >
                {[15, 30, 60].map((n) => (
                  <option key={n} value={n}>
                    {n} วิ
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <span className="interval muted">ล็อกแล้ว</span>
          )}
        </div>
        <button className="icon-btn" onClick={toggleFullscreen} aria-label={expanded ? "ออกจากเต็มจอ" : "เต็มจอ"}>
          {expanded ? <Minimize2 size={18} /> : <Maximize2 size={18} />}
        </button>
      </div>
      <div
        className={`grid ${single ? "one" : "four"}`}
        onTouchStart={(e) => {
          if (e.touches.length === 1 && !(e.target as HTMLElement).closest("input,button"))
            touch.current = { x: e.touches[0].clientX, y: e.touches[0].clientY };
        }}
        onTouchEnd={(e) => {
          const start = touch.current;
          touch.current = null;
          if (!start || !e.changedTouches.length) return;
          const dx = e.changedTouches[0].clientX - start.x,
            dy = e.changedTouches[0].clientY - start.y;
          if (Math.abs(dx) > 70 && Math.abs(dx) > Math.abs(dy) * 1.5) turn(dx < 0 ? 1 : -1);
        }}
      >
        {view.map((camera) => (
          <CameraTile
            key={camera.id}
            camera={camera}
            active={active}
            detailed={camera.id === detailId}
            audible={active && audibleId === camera.id}
            webRTC={webRTC}
            onOpen={() => void open(camera)}
            onAudio={() => onAudio(camera.id)}
            onRename={onRename}
            onEditing={onEditing}
          />
        ))}
        {!single &&
          Array.from({ length: Math.max(0, PAGE_SIZE - view.length) }, (_, i) => (
            <div className="tile empty" key={`empty-${i}`}>
              <span>ช่อง {view.length + i + 1} ว่าง</span>
            </div>
          ))}
        {single && !view.length && (
          <div className="tile empty">
            <span>ไม่พบกล้องในรายการนี้</span>
          </div>
        )}
      </div>
    </section>
  );
}
