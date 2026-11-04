export const PAGE_SIZE = 4;

export function pageCount(count: number) {
  return Math.max(1, Math.ceil(count / PAGE_SIZE));
}
export function clampPage(page: number, count: number) {
  return Math.min(Math.max(0, page), pageCount(count) - 1);
}
export function cameraPage<T>(cameras: readonly T[], page: number) {
  const start = clampPage(page, cameras.length) * PAGE_SIZE;
  return cameras.slice(start, start + PAGE_SIZE);
}
