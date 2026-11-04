import { useEffect, useRef, useState } from "react";
import { LoaderCircle, RefreshCw, ShieldAlert, WifiOff } from "lucide-react";
import type { Camera } from "./api";
import { apiUrl, statusLabel, tokenStore, usable, wsUrl } from "./api";
import { usePageVisible } from "./hooks";
import { reconnectDelay, requiresAccountFix } from "./reconnectPolicy";
import { streamProblem } from "./streamProblem";
import { startStream } from "./transport";
import type { StreamPhase, Transport } from "./transport";

export function Player({
  camera,
  quality = "low",
  audible = false,
  webRTC = false,
  active = true,
  onStateChange,
}: {
  camera: Camera;
  quality?: string;
  audible?: boolean;
  webRTC?: boolean;
  active?: boolean;
  onStateChange?: (s: string) => void;
}) {
  const video = useRef<HTMLVideoElement>(null);
  const [phase, setPhase] = useState<StreamPhase>("connecting");
  const [problem, setProblem] = useState("");
  const [transport, setTransport] = useState<Transport>();
  const [attempt, setAttempt] = useState(0);
  const [retries, setRetries] = useState(0);
  const visible = usePageVisible();
  const canPlay = usable(camera);
  const listener = useRef(onStateChange);
  listener.current = onStateChange;
  useEffect(() => {
    listener.current?.(
      !active || !visible ? "paused" : !canPlay ? "error" : phase,
    );
  }, [phase, active, visible, canPlay]);
  useEffect(() => {
    const el = video.current;
    if (!el) return;
    el.muted = !audible;
    if (audible) void el.play().catch(() => {});
  }, [audible]);
  useEffect(() => {
    if (phase !== "error" || !active || !visible || !canPlay) return;
    if (requiresAccountFix(problem)) return;
    const t = setTimeout(
      () => {
        setRetries((n) => Math.min(n + 1, 3));
        setAttempt((n) => n + 1);
      },
      reconnectDelay(retries, camera.id),
    );
    return () => clearTimeout(t);
  }, [phase, active, visible, canPlay, problem, retries, camera.id]);
  useEffect(() => {
    const el = video.current;
    if (!el || !canPlay || !visible || !active) return;
    setPhase("connecting");
    const base = `/cameras/${camera.id}`,
      q = `?quality=${quality}`;
    return startStream({
      el,
      wsUrl: wsUrl(`${base}/ws${q}`),
      hlsUrl: apiUrl(`${base}/hls${q}`),
      rtcUrl: apiUrl(`${base}/webrtc${q}`),
      order: webRTC ? ["webrtc", "mse", "hls"] : ["mse", "hls"],
      token: tokenStore.get(),
      onState: (p, d) => {
        setPhase(p);
        if (d.transport) setTransport(d.transport);
        if (p === "playing") {
          setProblem("");
          setRetries(0);
        } else if (d.problem) setProblem(d.problem);
      },
    });
  }, [camera.id, camera.playbackMode, canPlay, quality, attempt, webRTC, visible, active]);
  const issue = problem || (camera.status === "error" ? camera.message || "" : "");
  const info = issue ? streamProblem(issue) : undefined;
  return (
    <div className={`player${!canPlay || phase !== "playing" ? " player-problem" : ""}`}>
      <video ref={video} autoPlay muted={!audible} playsInline aria-label={`ภาพจาก ${camera.name}`} />
      {!canPlay ? (
        <div className="player-state">
          {camera.status === "auth_expired" ? <ShieldAlert /> : <WifiOff />}
          <strong>{statusLabel(camera.status)}</strong>
          <p>{camera.message || "ตรวจสอบกล้องและเครือข่าย"}</p>
        </div>
      ) : phase === "playing" ? (
        <span className="transport">{transport === "webrtc" ? "WebRTC" : transport?.toUpperCase()}</span>
      ) : phase === "connecting" && !info ? (
        <div className="player-state">
          <LoaderCircle className="spin" />
          <span>กำลังเปิดภาพ</span>
        </div>
      ) : (
        <div className="player-state">
          <WifiOff />
          <strong>{info?.title || "ยังเปิดภาพไม่ได้"}</strong>
          <p>{info?.help || camera.message || "ผลตรวจล่าสุดยังรับภาพไม่สำเร็จ"}</p>
          {phase === "connecting" ? (
            <small>กำลังลองเชื่อมต่อใหม่</small>
          ) : (
            <>
              {active && !requiresAccountFix(problem) && <small>จะลองใหม่อัตโนมัติ</small>}
              <button
                className="btn small"
                onClick={() => {
                  setRetries(0);
                  setAttempt((x) => x + 1);
                }}
              >
                <RefreshCw size={15} />
                ลองใหม่
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
