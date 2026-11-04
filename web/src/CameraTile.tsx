import { useState } from "react";
import { Volume2, VolumeX } from "lucide-react";
import type { Camera } from "./api";
import { CameraName } from "./CameraName";
import { cameraModel } from "./cameraPresentation";
import { Player } from "./Player";

export function CameraTile({
  camera,
  active,
  audible,
  webRTC,
  detailed,
  onOpen,
  onAudio,
  onRename,
  onEditing,
}: {
  camera: Camera;
  active: boolean;
  audible: boolean;
  webRTC: boolean;
  detailed: boolean;
  onOpen: () => void;
  onAudio: () => void;
  onRename?: (c: Camera, name: string) => Promise<void>;
  onEditing: (editing: boolean) => void;
}) {
  const [state, setState] = useState("connecting");
  const playing = state === "playing" && active;
  const pill = detailed
    ? "กำลังดูในแผงด้านข้าง"
    : playing
      ? camera.verification === "demo"
        ? "DEMO"
        : "LIVE"
      : state === "error"
        ? "ยังเปิดภาพไม่ได้"
        : !active
          ? "พักภาพ"
          : "กำลังเชื่อมต่อ";
  return (
    <article className="tile" data-camera-id={camera.id}>
      <div
        className="tile-video"
        onClick={(e) => {
          if (!(e.target as HTMLElement).closest("button, input")) onOpen();
        }}
      >
        {detailed ? (
          <div className="player player-problem">
            <div className="player-state">
              <span>{pill}</span>
            </div>
          </div>
        ) : (
          <Player camera={camera} audible={audible} active={active} webRTC={webRTC} onStateChange={setState} />
        )}
        <span className={`pill${playing && !detailed ? " live" : ""}`}>
          <i />
          {pill}
        </span>
        {camera.audio && !detailed && (
          <button
            className="tile-audio"
            onClick={onAudio}
            disabled={!playing}
            aria-label={`${audible ? "ปิด" : "เปิด"}เสียง ${camera.name}`}
            aria-pressed={audible}
          >
            {audible ? <Volume2 size={18} /> : <VolumeX size={18} />}
          </button>
        )}
        <button className="tile-open" onClick={onOpen} aria-label={`เปิดรายละเอียด ${camera.name}`} />
      </div>
      <div className="tile-caption">
        <CameraName camera={camera} onSave={onRename} onEditing={onEditing} />
        <span className="model">{cameraModel(camera)}</span>
      </div>
    </article>
  );
}
