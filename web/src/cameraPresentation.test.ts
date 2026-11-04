import { expect, it } from "vitest";
import { cameraModel } from "./cameraPresentation";
import type { Camera } from "./api";

it("identifies each lens independently of an editable camera name", () => {
  const dual = { model: "chuangmi.camera.072ae2", name: "โรงฝึก", lensCount: 2 };
  expect(cameraModel({ ...dual, lens: 1 } as Camera)).toBe("Xiaomi C300 Dual · เลนส์ 1/2");
  expect(cameraModel({ ...dual, lens: 2 } as Camera)).toBe("Xiaomi C300 Dual · เลนส์ 2/2");
  expect(cameraModel({ model: "xiaomi.camera.c01a01", name: "Dual" } as Camera)).toBe("Xiaomi C300");
});
