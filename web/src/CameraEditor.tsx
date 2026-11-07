import { useEffect, useId, useRef, useState } from "react";
import type { Camera } from "./api";
import { Spinner } from "./ui";

export interface CameraEdit {
  name: string;
  group: string;
}

/** Small inline form under a picker row: rename and move to a group in one save. */
export function CameraEditor({
  camera,
  groups,
  onSave,
  onClose,
}: {
  camera: Camera;
  groups: string[];
  onSave: (camera: Camera, edit: CameraEdit) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(camera.name);
  const [group, setGroup] = useState(camera.group);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const first = useRef<HTMLInputElement>(null);
  const listId = useId();
  useEffect(() => {
    first.current?.focus();
    first.current?.select();
  }, []);
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const edit = { name: name.trim(), group: group.trim() };
    if (!edit.name) {
      setError("ชื่อกล้องห้ามว่าง");
      return;
    }
    if (edit.name === camera.name && edit.group === camera.group) {
      onClose();
      return;
    }
    setBusy(true);
    setError("");
    try {
      await onSave(camera, edit);
      onClose();
    } catch (err) {
      setError((err as Error).message || "บันทึกไม่ได้ ลองอีกครั้ง");
      setBusy(false);
    }
  };
  const known = groups.filter(Boolean);
  const others = known.filter((g) => g !== group);
  return (
    <form
      className="picker-edit"
      aria-label={`แก้ไข ${camera.name}`}
      onSubmit={(e) => void submit(e)}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === "Escape") {
          e.preventDefault();
          onClose();
        }
      }}
    >
      <label>
        ชื่อกล้อง
        <input ref={first} value={name} maxLength={60} autoComplete="off" spellCheck={false} readOnly={busy} onChange={(e) => setName(e.target.value)} />
      </label>
      <label>
        กลุ่ม / ห้อง
        <input
          list={listId}
          value={group}
          maxLength={60}
          autoComplete="off"
          placeholder="เช่น ชั้นล่าง, หน้าบ้าน"
          readOnly={busy}
          onChange={(e) => setGroup(e.target.value)}
        />
        <datalist id={listId}>
          {known.map((g) => (
            <option key={g} value={g} />
          ))}
        </datalist>
      </label>
      {others.length > 0 && (
        <div className="picker-groups" aria-label="เลือกกลุ่มที่มีอยู่">
          {others.map((g) => (
            <button key={g} type="button" className="chip" disabled={busy} onClick={() => setGroup(g)}>
              {g}
            </button>
          ))}
        </div>
      )}
      {error && (
        <span className="name-error" role="alert">
          {error}
        </span>
      )}
      <div className="row">
        <button className="btn primary small" disabled={busy}>
          {busy ? <Spinner size={14} /> : null}
          บันทึก
        </button>
        <button type="button" className="btn small" disabled={busy} onClick={onClose}>
          ยกเลิก
        </button>
      </div>
    </form>
  );
}
