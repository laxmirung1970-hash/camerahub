import { describe, expect, it } from "vitest";
import { cameraPage, clampPage, pageCount } from "./monitoring";

describe("all cameras remain available in a four-camera tour", () => {
  const cameras = Array.from({ length: 17 }, (_, id) => ({ id }));
  it("visits every camera once, including the last partial page", () => {
    const pages = Array.from({ length: pageCount(cameras.length) }, (_, p) => cameraPage(cameras, p));
    expect(pages.map((p) => p.length)).toEqual([4, 4, 4, 4, 1]);
    expect(pages.flat().map((c) => c.id)).toEqual(cameras.map((c) => c.id));
  });
  it("clamps out-of-range pages and keeps an empty collection valid", () => {
    expect(clampPage(9, 17)).toBe(4);
    expect(clampPage(-1, 17)).toBe(0);
    expect(cameraPage(cameras.slice(0, 3), 4)).toHaveLength(3);
    expect(cameraPage([], 8)).toEqual([]);
  });
});
