import type { Camera } from "./api";
import { cameraPage, clampPage, pageCount } from "./monitoring";

export type DisplayMode = "tour4" | "tour1" | "lock4" | "focus";
export interface DisplayState {
  mode: DisplayMode;
  page: number;
  focusId: string;
  lockedIds: string[] | null;
}
export const initialDisplay: DisplayState = {
  mode: "tour4",
  page: 0,
  focusId: "",
  lockedIds: null,
};
export const isSingle = (mode: DisplayMode) =>
  mode === "tour1" || mode === "focus";
export const isTour = (mode: DisplayMode) =>
  mode === "tour4" || mode === "tour1";
export function displayed(cameras: Camera[], state: DisplayState): Camera[] {
  if (isSingle(state.mode))
    return cameras.length
      ? [
          cameras.find((c) => c.id === state.focusId) ||
            cameraPage(cameras, state.page)[0],
        ]
      : [];
  if (state.mode === "lock4" && state.lockedIds !== null)
    return state.lockedIds
      .flatMap((id) => cameras.find((c) => c.id === id) || [])
      .slice(0, 4);
  return cameraPage(cameras, state.page);
}
export function chooseMode(
  cameras: Camera[],
  state: DisplayState,
  mode: DisplayMode,
): DisplayState {
  const visible = displayed(cameras, state);
  const first =
    cameras.find(
      (c) => c.id === state.focusId && visible.some((v) => v.id === c.id),
    ) ||
    visible[0] ||
    cameras[0];
  return {
    mode,
    page: first ? Math.floor(cameras.indexOf(first) / 4) : 0,
    focusId: first?.id || "",
    lockedIds:
      mode === "lock4"
        ? isSingle(state.mode)
          ? cameraPage(
              cameras,
              Math.floor(Math.max(0, cameras.indexOf(first)) / 4),
            ).map((c) => c.id)
          : visible.map((c) => c.id)
        : null,
  };
}
export function chooseStep(
  cameras: Camera[],
  state: DisplayState,
  index: number,
): DisplayState {
  if (isSingle(state.mode)) {
    const position = Math.min(
      Math.max(0, index),
      Math.max(0, cameras.length - 1),
    );
    return {
      ...state,
      page: Math.floor(position / 4),
      focusId: cameras[position]?.id || "",
      lockedIds: null,
    };
  }
  const page = clampPage(index, cameras.length);
  return {
    ...state,
    page,
    focusId: cameraPage(cameras, page)[0]?.id || "",
    lockedIds:
      state.mode === "lock4"
        ? cameraPage(cameras, page).map((c) => c.id)
        : null,
  };
}
export function advance(
  cameras: Camera[],
  state: DisplayState,
  step: number,
): DisplayState {
  const total = isSingle(state.mode)
    ? Math.max(1, cameras.length)
    : pageCount(cameras.length);
  const current = isSingle(state.mode)
    ? Math.max(
        0,
        cameras.findIndex((c) => c.id === displayed(cameras, state)[0]?.id),
      )
    : clampPage(state.page, cameras.length);
  return chooseStep(
    cameras,
    state,
    (((current + step) % total) + total) % total,
  );
}
export function focusCamera(
  cameras: Camera[],
  state: DisplayState,
  id: string,
): DisplayState {
  const index = cameras.findIndex((c) => c.id === id);
  return index < 0
    ? state
    : {
        ...state,
        mode: "focus",
        focusId: id,
        page: Math.floor(index / 4),
        lockedIds: null,
      };
}
export function toggleLocked(
  cameras: Camera[],
  state: DisplayState,
  id: string,
): DisplayState {
  const ids =
    state.mode === "lock4" ? displayed(cameras, state).map((c) => c.id) : [];
  const next = ids.includes(id)
    ? ids.filter((x) => x !== id)
    : ids.length < 4
      ? [...ids, id]
      : ids;
  return { ...state, mode: "lock4", lockedIds: next };
}
/** Moves within the visible collection while keeping filtered-out registry entries in their slots. */
export function reordered<T extends { id: string }>(
  all: T[],
  visible: T[],
  id: string,
  targetIndex: number,
): T[] {
  const from = visible.findIndex((c) => c.id === id);
  if (from < 0) return all;
  const next = [...visible],
    [item] = next.splice(from, 1);
  next.splice(Math.max(0, Math.min(targetIndex, next.length)), 0, item);
  const ids = new Set(visible.map((c) => c.id));
  let index = 0;
  return all.map((c) => (ids.has(c.id) ? next[index++] : c));
}
