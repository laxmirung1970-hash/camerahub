export const PAGE_SIZE = 4;

export interface Groupable {
  group?: string;
}
const groupOf = (c: Groupable) => c.group ?? "";

/**
 * Owner order with each group made contiguous: groups appear in the order of
 * their first camera, cameras keep their order inside the group. Moving a
 * camera to the top of the list therefore also moves its group first.
 */
export function groupedOrder<T extends Groupable>(cameras: readonly T[]): T[] {
  const order: string[] = [];
  for (const c of cameras) if (!order.includes(groupOf(c))) order.push(groupOf(c));
  return order.flatMap((g) => cameras.filter((c) => groupOf(c) === g));
}

/** Pages of up to four. A page never mixes groups: each group starts a new page. */
export function pagesOf<T extends Groupable>(cameras: readonly T[]): T[][] {
  const pages: T[][] = [];
  let page: T[] = [];
  for (const c of cameras) {
    if (page.length === PAGE_SIZE || (page.length > 0 && groupOf(page[0]) !== groupOf(c))) {
      pages.push(page);
      page = [];
    }
    page.push(c);
  }
  if (page.length) pages.push(page);
  return pages;
}
export function pageCount(cameras: readonly Groupable[]) {
  return Math.max(1, pagesOf(cameras).length);
}
export function clampPage(page: number, cameras: readonly Groupable[]) {
  return Math.min(Math.max(0, page), pageCount(cameras) - 1);
}
export function cameraPage<T extends Groupable>(cameras: readonly T[], page: number): T[] {
  return pagesOf(cameras)[clampPage(page, cameras)] ?? [];
}
/** Page that holds the camera at `index` of the list; 0 when the index is out of range. */
export function pageOf(cameras: readonly Groupable[], index: number) {
  if (index < 0 || index >= cameras.length) return 0;
  let seen = 0;
  const pages = pagesOf(cameras);
  for (let p = 0; p < pages.length; p++) {
    seen += pages[p].length;
    if (index < seen) return p;
  }
  return 0;
}
