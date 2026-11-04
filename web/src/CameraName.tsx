import { useEffect, useRef, useState } from "react";
import type { Camera } from "./api";

export function CameraName({
  camera,
  onSave,
  onEditing,
}: {
  camera: Camera;
  onSave?: (camera: Camera, name: string) => Promise<void>;
  onEditing?: (active: boolean) => void;
}) {
  const [draft, setDraft] = useState(camera.name);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const editing = useRef(false),
    cancel = useRef(false),
    saving = useRef(false);
  useEffect(
    () => () => {
      if (editing.current) onEditing?.(false);
    },
    [onEditing],
  );
  useEffect(() => {
    if (!editing.current && !saving.current && !error) setDraft(camera.name);
  }, [camera.name, error]);
  if (!onSave)
    return <strong className="readonly-camera-name">{camera.name}</strong>;
  const commit = async () => {
    editing.current = false;
    onEditing?.(false);
    if (cancel.current) {
      cancel.current = false;
      return;
    }
    if (saving.current) return;
    const name = draft.trim();
    if (!name) {
      setDraft(camera.name);
      setError("ชื่อกล้องห้ามว่าง");
      return;
    }
    if (name === camera.name) {
      setDraft(name);
      return;
    }
    saving.current = true;
    setBusy(true);
    setError("");
    try {
      await onSave(camera, name);
      setDraft(name);
    } catch (e) {
      setError((e as Error).message || "บันทึกชื่อไม่ได้ ลองอีกครั้ง");
    } finally {
      saving.current = false;
      setBusy(false);
    }
  };
  return (
    <div className={`inline-camera-name ${busy ? "is-saving" : ""}`}>
      <input
        aria-label={`ชื่อกล้อง ${camera.name}`}
        title={`${camera.name} · พิมพ์แก้ชื่อ · Enter บันทึก · Esc ยกเลิก`}
        value={draft}
        maxLength={60}
        autoComplete="off"
        spellCheck={false}
        readOnly={busy}
        onFocus={() => {
          editing.current = true;
          onEditing?.(true);
        }}
        onChange={(e) => {
          setDraft(e.target.value);
          setError("");
        }}
        onBlur={() => void commit()}
        onKeyDown={(e) => {
          e.stopPropagation();
          if (e.key === "Enter") {
            e.preventDefault();
            e.currentTarget.blur();
          }
          if (e.key === "Escape") {
            e.preventDefault();
            cancel.current = true;
            setDraft(camera.name);
            setError("");
            e.currentTarget.blur();
          }
        }}
        aria-invalid={Boolean(error)}
        aria-busy={busy}
      />
      {busy && <span className="name-feedback">กำลังบันทึก…</span>}
      {error && (
        <span className="name-error" role="alert">
          {error}
        </span>
      )}
    </div>
  );
}
