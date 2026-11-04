import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { AlertCircle, Camera as CameraIcon, CheckCircle2, LoaderCircle, X } from "lucide-react";

export function Logo({ compact = false }: { compact?: boolean }) {
  return (
    <div className="brand">
      <span className="brand-symbol">
        <CameraIcon size={18} />
      </span>
      {!compact && <strong>CameraHub</strong>}
    </div>
  );
}
export const Spinner = ({ size = 18 }: { size?: number }) => (
  <LoaderCircle className="spin" size={size} aria-hidden />
);
const FOCUSABLE =
  "button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href],summary,[tabindex]:not([tabindex='-1'])";
export function Modal({
  title,
  children,
  onClose,
  wide = false,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  const root = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    root.current?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        closeRef.current();
        return;
      }
      if (e.key !== "Tab") return;
      const items = root.current?.querySelectorAll<HTMLElement>(FOCUSABLE);
      if (!items?.length) return;
      const first = items[0],
        last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", key, true);
    const old = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", key, true);
      document.body.style.overflow = old;
      prev?.focus();
    };
  }, []);
  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={root} className={`modal${wide ? " wide" : ""}`} role="dialog" aria-modal="true" aria-label={title} tabIndex={-1}>
        <header className="modal-head">
          <h2>{title}</h2>
          <button className="icon-btn" aria-label="ปิดหน้าต่าง" onClick={onClose}>
            <X size={18} />
          </button>
        </header>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  );
}
export function Notice({ children, warning = false }: { children: ReactNode; warning?: boolean }) {
  return (
    <div className={`notice${warning ? " warning" : ""}`} role={warning ? "alert" : "status"}>
      <AlertCircle size={16} />
      <div>{children}</div>
    </div>
  );
}
export type ToastMessage = { text: string; kind: "ok" | "error" };
export function Toast({ toast, onClose }: { toast: ToastMessage; onClose: () => void }) {
  return (
    <div className={`toast ${toast.kind}`} role="status">
      {toast.kind === "ok" ? <CheckCircle2 size={16} /> : <AlertCircle size={16} />}
      <span>{toast.text}</span>
      <button className="icon-btn" aria-label="ปิดข้อความ" onClick={onClose}>
        <X size={14} />
      </button>
    </div>
  );
}
