import { describe, expect, it } from "vitest";
import type { Camera } from "./api";
import {
  advance,
  chooseMode,
  chooseStep,
  displayed,
  focusCamera,
  initialDisplay,
  reordered,
  toggleLocked,
} from "./displayState";
const cameras = Array.from(
  { length: 17 },
  (_, i) =>
    ({
      id: String(i),
      name: `Camera ${i}`,
      status: i % 4 === 0 ? "error" : "online",
    }) as Camera,
);
describe("compact monitor modes", () => {
  it("opens four and cycles all 17 including problem cameras", () => {
    let state = initialDisplay;
    const ids: string[] = [];
    for (let i = 0; i < 5; i++) {
      ids.push(...displayed(cameras, state).map((c) => c.id));
      state = advance(cameras, state, 1);
    }
    expect(ids).toEqual(cameras.map((c) => c.id));
    expect(state.page).toBe(0);
  });
  it("single tour visits every camera and wraps backwards and forwards", () => {
    let state = chooseMode(cameras, initialDisplay, "tour1");
    const ids = [];
    for (let i = 0; i < 17; i++) {
      ids.push(displayed(cameras, state)[0].id);
      state = advance(cameras, state, 1);
    }
    expect(ids).toEqual(cameras.map((c) => c.id));
    expect(advance(cameras, state, -1).focusId).toBe("16");
  });
  it("switching to lock captures the current four despite catalog reordering", () => {
    const locked = chooseMode(
      cameras,
      chooseStep(cameras, initialDisplay, 2),
      "lock4",
    );
    expect(locked.lockedIds).toEqual(["8", "9", "10", "11"]);
    expect(displayed([...cameras].reverse(), locked).map((c) => c.id)).toEqual(
      locked.lockedIds,
    );
    const focus = focusCamera(cameras, locked, "10");
    expect(focus.mode).toBe("focus");
    expect(displayed(cameras, focus).map((c) => c.id)).toEqual(["10"]);
  });
  it("first checkbox starts a custom set; fifth never removes an existing choice", () => {
    let state = toggleLocked(cameras, initialDisplay, "9");
    expect(state.lockedIds).toEqual(["9"]);
    for (const id of ["2", "15", "4", "5"])
      state = toggleLocked(cameras, state, id);
    expect(state.lockedIds).toEqual(["9", "2", "15", "4"]);
    state = toggleLocked(cameras, state, "2");
    state = toggleLocked(cameras, state, "5");
    expect(state.lockedIds).toEqual(["9", "15", "4", "5"]);
  });
  it("empty and shrinking catalogs stay valid in every mode", () => {
    for (const mode of ["tour4", "tour1", "lock4", "focus"] as const) {
      const state = chooseMode([], initialDisplay, mode);
      expect(displayed([], advance([], state, 1))).toEqual([]);
      expect(
        displayed(cameras.slice(0, 2), chooseStep(cameras, state, 16)).length,
      ).toBeLessThanOrEqual(4);
    }
  });
  it("moves across four-camera pages and preserves hidden registry slots", () => {
    const next = reordered(cameras, cameras, "3", 4);
    expect(next.slice(0, 6).map((c) => c.id)).toEqual([
      "0",
      "1",
      "2",
      "4",
      "3",
      "5",
    ]);
    expect(cameras[3].id).toBe("3");
    const filtered = [cameras[1], cameras[4], cameras[8]];
    const changed = reordered(cameras, filtered, "8", 0);
    expect([changed[1].id, changed[4].id, changed[8].id]).toEqual([
      "8",
      "1",
      "4",
    ]);
    expect(
      changed.filter((c) => !filtered.includes(c)).map((c) => c.id),
    ).toEqual(cameras.filter((c) => !filtered.includes(c)).map((c) => c.id));
  });
});
