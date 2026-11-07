import { describe, expect, it } from "vitest";
import { cameraPage, clampPage, groupedOrder, pageCount, pageOf, pagesOf } from "./monitoring";

describe("all cameras remain available in a four-camera tour", () => {
  const cameras = Array.from({ length: 17 }, (_, id) => ({ id, group: "" }));
  it("visits every camera once, including the last partial page", () => {
    const pages = Array.from({ length: pageCount(cameras) }, (_, p) => cameraPage(cameras, p));
    expect(pages.map((p) => p.length)).toEqual([4, 4, 4, 4, 1]);
    expect(pages.flat().map((c) => c.id)).toEqual(cameras.map((c) => c.id));
  });
  it("clamps out-of-range pages and keeps an empty collection valid", () => {
    expect(clampPage(9, cameras)).toBe(4);
    expect(clampPage(-1, cameras)).toBe(0);
    expect(cameraPage(cameras.slice(0, 3), 4)).toHaveLength(3);
    expect(cameraPage([], 8)).toEqual([]);
    expect(pageCount([])).toBe(1);
    expect(pageOf(cameras, 5)).toBe(1);
    expect(pageOf(cameras, 16)).toBe(4);
    expect(pageOf(cameras, 99)).toBe(0);
  });
});

describe("pages follow groups", () => {
  const cam = (id: string, group: string) => ({ id, group });
  const owner = [cam("a1", "ชั้นล่าง"), cam("b1", "ชั้นบน"), cam("a2", "ชั้นล่าง"), cam("a3", "ชั้นล่าง"), cam("a4", "ชั้นล่าง"), cam("a5", "ชั้นล่าง"), cam("c1", ""), cam("b2", "ชั้นบน")];
  it("keeps owner order inside each group and orders groups by their first camera", () => {
    expect(groupedOrder(owner).map((c) => c.id)).toEqual(["a1", "a2", "a3", "a4", "a5", "b1", "b2", "c1"]);
    expect(groupedOrder([]).length).toBe(0);
  });
  it("never puts two groups on one page, so a five-camera group spans two pages", () => {
    const pages = pagesOf(groupedOrder(owner));
    expect(pages.map((p) => p.map((c) => c.id))).toEqual([["a1", "a2", "a3", "a4"], ["a5"], ["b1", "b2"], ["c1"]]);
    expect(pageOf(groupedOrder(owner), 5)).toBe(2);
    expect(pageOf(groupedOrder(owner), 7)).toBe(3);
  });
  it("treats a missing group like an empty one and keeps a single-group list in plain fours", () => {
    expect(pagesOf([{ id: "x", group: undefined }, cam("y", ""), { id: "z", group: undefined }]).length).toBe(1);
    expect(pagesOf(Array.from({ length: 9 }, (_, i) => cam(String(i), "g"))).map((p) => p.length)).toEqual([4, 4, 1]);
  });
});
