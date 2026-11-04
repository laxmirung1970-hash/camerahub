import { useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp } from "lucide-react";
import type { Camera } from "./api";
import { api, usable } from "./api";

const DIRECTIONS = [
  ["up", "ขึ้น", ChevronUp],
  ["left", "ซ้าย", ChevronLeft],
  ["right", "ขวา", ChevronRight],
  ["down", "ลง", ChevronDown],
] as const;

/** Hold to move. Requests are serialised so a stop can never overtake a move. */
export function PTZPad({ camera, onError }: { camera: Camera; onError: (s: string) => void }) {
  const [dir, setDir] = useState("");
  const queue = useRef(Promise.resolve());
  const moving = useRef(false);
  const timer = useRef<ReturnType<typeof setInterval>>(undefined);
  const send = (direction: string) => {
    queue.current = queue.current
      .then(() => api(`/cameras/${camera.id}/ptz`, { direction }))
      .then(() => undefined)
      .catch((e: Error) => {
        onError(e.message);
        stop();
      });
  };
  const stop = () => {
    clearInterval(timer.current);
    setDir("");
    if (!moving.current) return;
    moving.current = false;
    send("stop");
  };
  const start = (direction: string) => {
    if (moving.current) return;
    moving.current = true;
    setDir(direction);
    send(direction);
    timer.current = setInterval(() => send(direction), 350);
  };
  useEffect(() => {
    const hide = () => {
      if (document.hidden) stop();
    };
    document.addEventListener("visibilitychange", hide);
    return () => {
      document.removeEventListener("visibilitychange", hide);
      stop();
    };
    // stop() only reads refs; camera.id changes remount via key in CameraDetail.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const disabled = !camera.ptz || !usable(camera);
  return (
    <div className="ptz" aria-label="หมุนกล้อง">
      {DIRECTIONS.map(([d, label, Icon]) => (
        <button
          key={d}
          className={`ptz-${d}${dir === d ? " pressed" : ""}`}
          aria-label={`หมุน${label}`}
          disabled={disabled}
          onPointerDown={(e) => {
            e.preventDefault();
            e.currentTarget.setPointerCapture(e.pointerId);
            start(d);
          }}
          onPointerUp={stop}
          onPointerCancel={stop}
          onKeyDown={(e) => {
            if ((e.key === " " || e.key === "Enter") && !e.repeat) {
              e.preventDefault();
              start(d);
            }
          }}
          onKeyUp={(e) => {
            if (e.key === " " || e.key === "Enter") stop();
          }}
        >
          <Icon size={22} />
        </button>
      ))}
      <button className="ptz-center" aria-label="หยุดหมุน" disabled={disabled} onClick={stop}>
        <i />
      </button>
    </div>
  );
}
