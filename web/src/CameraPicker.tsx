import { useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronUp, GripVertical, Pencil, Scan } from "lucide-react";
import type { Camera } from "./api";
import { statusLabel } from "./api";
import type { CameraEdit } from "./CameraEditor";
import { CameraEditor } from "./CameraEditor";
import { cameraModel } from "./cameraPresentation";
import { PAGE_SIZE, pageCount } from "./monitoring";

/** Flat list with a header per page of four, so ↑↓ and drag work across pages. */
export function CameraPicker({
  cameras,
  groups,
  selected,
  showing,
  owner,
  ordering,
  bridgeReady,
  onShowPage,
  onToggle,
  onFocus,
  onEdit,
  onEditing,
  onMoveTo,
}: {
  cameras: Camera[];
  groups: string[];
  selected: string[];
  showing: string[];
  owner: boolean;
  ordering: boolean;
  bridgeReady: boolean;
  onShowPage: (page: number) => void;
  onToggle: (id: string) => void;
  onFocus: (c: Camera) => void;
  onEdit?: (c: Camera, edit: CameraEdit) => Promise<void>;
  onEditing: (editing: boolean) => void;
  onMoveTo: (id: string, index: number) => void;
}) {
  const [dragging, setDragging] = useState("");
  const [editId, setEditId] = useState("");
  // The tour pauses while an editor is open; the row vanishing (filter, hide) also closes it.
  const editing = Boolean(editId) && cameras.some((c) => c.id === editId);
  useEffect(() => {
    onEditing(editing);
    return () => onEditing(false);
  }, [editing, onEditing]);
  const list = useRef<HTMLDivElement>(null);
  const restore = useRef<{ id: string; direction: string } | null>(null);
  const signature = cameras.map((c) => c.id).join("|");
  useEffect(() => {
    if (ordering || !restore.current) return;
    const pending = restore.current;
    restore.current = null;
    const row = list.current?.querySelector<HTMLElement>(`[data-picker-id="${CSS.escape(pending.id)}"]`);
    if (!row) return;
    const wanted = row.querySelector<HTMLButtonElement>(`[data-order-step="${pending.direction}"]`);
    (wanted && !wanted.disabled ? wanted : row.querySelector<HTMLButtonElement>("[data-order-step]:not(:disabled)"))?.focus({
      preventScroll: true,
    });
    row.scrollIntoView({ block: "nearest" });
  }, [ordering, signature]);
  const pages = pageCount(cameras.length);
  return (
    <div ref={list} className="picker-list" aria-label="รายชื่อกล้อง">
      {cameras.length === 0 && <p className="muted">ไม่พบกล้องที่ตรงกับการค้นหา</p>}
      {cameras.map((c, index) => {
        const page = Math.floor(index / PAGE_SIZE);
        const checked = selected.includes(c.id);
        const onScreen = showing.includes(c.id);
        return (
          <div key={c.id}>
            {index % PAGE_SIZE === 0 && (
              <div className="picker-page">
                <span>หน้า {page + 1}{pages > 1 ? ` / ${pages}` : ""}</span>
                <button className="text-btn" onClick={() => onShowPage(page)}>
                  แสดง
                </button>
              </div>
            )}
            <div
              className={`picker-row${onScreen ? " on-screen" : ""}${dragging === c.id ? " dragging" : ""}`}
              data-picker-id={c.id}
              onDragOver={(e) => {
                if (dragging && dragging !== c.id) {
                  e.preventDefault();
                  e.dataTransfer.dropEffect = "move";
                }
              }}
              onDrop={(e) => {
                e.preventDefault();
                if (dragging && cameras.some((x) => x.id === dragging)) onMoveTo(dragging, index);
                setDragging("");
              }}
            >
              <input
                type="checkbox"
                checked={checked}
                aria-label={`เลือก ${c.name} ลงชุดล็อก 4 ภาพ`}
                title="เลือกลงชุดล็อก 4 ภาพ"
                onChange={() => onToggle(c.id)}
              />
              <div className="picker-body">
                <strong className="readonly-camera-name">{c.name}</strong>
                <small>
                  {c.group && <span className="picker-group">{c.group} · </span>}
                  {cameraModel(c)} · <i className={`dot ${bridgeReady ? c.status : "pending"}`} />
                  {bridgeReady ? statusLabel(c.status) : "ระบบวิดีโอไม่พร้อม"}
                  {onScreen && " · บนจอ"}
                </small>
              </div>
              <button className="icon-btn" aria-label={`โฟกัส ${c.name}`} title="ดูภาพใหญ่กล้องนี้" onClick={() => onFocus(c)}>
                <Scan size={17} />
              </button>
              {owner && (
                <>
                  {onEdit && (
                    <button
                      className={`icon-btn${editId === c.id ? " active" : ""}`}
                      aria-label={`แก้ชื่อและกลุ่ม ${c.name}`}
                      aria-expanded={editId === c.id}
                      title="แก้ชื่อและกลุ่ม"
                      onClick={() => setEditId(editId === c.id ? "" : c.id)}
                    >
                      <Pencil size={16} />
                    </button>
                  )}
                  <button
                    className="icon-btn"
                    data-order-step="up"
                    disabled={ordering || index === 0}
                    aria-label={`เลื่อน ${c.name} ขึ้น`}
                    onClick={() => {
                      restore.current = { id: c.id, direction: "up" };
                      onMoveTo(c.id, index - 1);
                    }}
                  >
                    <ChevronUp size={17} />
                  </button>
                  <button
                    className="icon-btn"
                    data-order-step="down"
                    disabled={ordering || index === cameras.length - 1}
                    aria-label={`เลื่อน ${c.name} ลง`}
                    onClick={() => {
                      restore.current = { id: c.id, direction: "down" };
                      onMoveTo(c.id, index + 1);
                    }}
                  >
                    <ChevronDown size={17} />
                  </button>
                  <button
                    className="icon-btn grip"
                    draggable={!ordering}
                    tabIndex={-1}
                    aria-hidden
                    title="ลากเพื่อย้าย"
                    onDragStart={(e) => {
                      setDragging(c.id);
                      e.dataTransfer.setData("text/plain", c.id);
                      e.dataTransfer.effectAllowed = "move";
                    }}
                    onDragEnd={() => setDragging("")}
                  >
                    <GripVertical size={15} />
                  </button>
                </>
              )}
            </div>
            {onEdit && editId === c.id && <CameraEditor camera={c} groups={groups} onSave={onEdit} onClose={() => setEditId("")} />}
          </div>
        );
      })}
    </div>
  );
}
