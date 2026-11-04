import type { Camera } from "./api";

const models: Record<string, string> = {
  "xiaomi.camera.c01a01": "Xiaomi C300",
  "xiaomi.camera.c302n": "Xiaomi C302",
  "chuangmi.camera.072ae2": "Xiaomi C300 Dual",
  "chuangmi.camera.046a01": "Mi 360° 1080p",
};
export function cameraModel(camera: Camera) {
  const model = models[camera.model] || camera.model || "ยังไม่ทราบรุ่น";
  return camera.lensCount === 2 && (camera.lens === 1 || camera.lens === 2)
    ? `${model} · เลนส์ ${camera.lens}/2`
    : model;
}
