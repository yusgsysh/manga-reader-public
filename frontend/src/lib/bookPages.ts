/**
 * Comic pages ↔ grabfold sheets.
 *
 * grabfold models a book as sheets, each with a front and a back:
 * an open spread shows the back of one sheet beside the front of the next.
 * Manga is a flat list of independent images, so this module is the only place
 * that decides which image belongs on which face, and where the book must be
 * open (its "position") to show a given comic page.
 *
 * Two things make the mapping layout-specific:
 *
 * - A spread seats two comic pages on one sheet, so `sheets = total / 2`.
 * - grabfold's single layout only shows sheet *fronts* — the backs pass by on
 *   the fold — so there every sheet must carry exactly one comic page.
 *
 * The sheet count therefore changes with the layout, and a layout switch has
 * to rebuild the book at a position derived from the comic page, never from
 * the old grabfold position. Comic page indexes stay 0-based everywhere; a
 * grabfold position is never treated as a comic page.
 *
 * With covers ("显示封面页") the first comic page sits alone on the right, as a
 * book's first page does; then spreads pair (1,2), (3,4)…  Without it the
 * first spread pairs (0,1), (2,3)… and the first sheet's front is the blank
 * inside cover that page 0's back would otherwise be printed on.
 */

import type { PageRef } from "grabfold";

export type ReadingDirection = "rtl" | "ltr";
export type BookLayout = "spread" | "single";
export type BookCovers = "none" | "inside";
/** A step through the comic, in reading order. */
export type PageTurn = "next" | "previous";

export interface BookPageMap {
  /** Comic pages, at least 1. */
  pages: number;
  layout: BookLayout;
  hasCover: boolean;
  /** Sheets grabfold is given. */
  sheets: number;
  covers: BookCovers;
  /** Lowest and highest grabfold position this layout can open at. */
  first: number;
  last: number;
}

/** How many comic pages one sheet holds in this layout. */
export function pagesPerSheet(layout: BookLayout): 1 | 2 {
  return layout === "single" ? 1 : 2;
}

/**
 * The book shape for a comic, in a layout.
 *
 * In a spread the last sheet is kept when the page count is even: the final
 * comic page is the back of the last real sheet, and showing it needs one more
 * (blank) sheet's front beside it, exactly as a printed book ends.
 */
export function bookPageMap(
  total: number,
  layout: BookLayout,
  hasCover: boolean,
): BookPageMap {
  const pages = Math.max(1, Math.floor(total));

  if (layout === "single") {
    return {
      pages,
      layout,
      hasCover,
      sheets: pages,
      covers: "none",
      first: -1,
      // The last page is the front of the last sheet: position pages - 2.
      last: Math.max(-1, pages - 2),
    };
  }

  const sheets = Math.floor(pages / 2) + 1;
  const covers: BookCovers = hasCover ? "none" : "inside";
  const first = hasCover ? -1 : 0;
  const last = hasCover
    ? Math.max(-1, sheets - 2)
    : pages % 2 === 0
      ? pages / 2 - 1
      : Math.floor(pages / 2);

  return { pages, layout, hasCover, sheets, covers, first, last };
}

export function clampPage(pageIndex: number, pages: number): number {
  if (pages <= 0) return 0;
  return Math.min(Math.max(Math.floor(pageIndex), 0), pages - 1);
}

export function clampBookPosition(map: BookPageMap, position: number): number {
  if (!Number.isFinite(position)) return map.first;
  return Math.min(map.last, Math.max(map.first, Math.floor(position)));
}

/** The grabfold position that shows `pageIndex`. */
export function positionForPage(map: BookPageMap, pageIndex: number): number {
  const page = clampPage(pageIndex, map.pages);

  if (map.layout === "single") return page - 1;
  if (map.hasCover) return page === 0 ? -1 : Math.floor((page - 1) / 2);
  return Math.floor(page / 2);
}

/**
 * The comic page a position reads from — its anchor.
 *
 * A spread shows two pages; the anchor is the one read first (the back of the
 * sheet on the spine side), so saving it and opening at its position again
 * restores the same spread.
 */
export function pageForPosition(map: BookPageMap, position: number): number {
  const at = clampBookPosition(map, position);

  if (map.layout === "single") return clampPage(at + 1, map.pages);
  if (map.hasCover) return at < 0 ? 0 : clampPage(2 * at + 1, map.pages);
  return clampPage(2 * at, map.pages);
}

/** The comic page a grabfold page reference shows; null for blank paper. */
export function pageForRef(map: BookPageMap, ref: PageRef | null): number | null {
  if (!ref || ref.kind === "cover") return null;
  const { sheet } = ref;
  if (!Number.isInteger(sheet) || sheet < 0 || sheet >= map.sheets) return null;

  if (map.layout === "single") {
    // Single layout shows fronts only: its backs are the paper seen mid-turn.
    return ref.kind === "front" && sheet < map.pages ? sheet : null;
  }

  if (map.hasCover) {
    const page = ref.kind === "front" ? sheet * 2 : sheet * 2 + 1;
    return page < map.pages ? page : null;
  }

  // Sheet 0's front is the blank inside cover; page 0 is printed on its back.
  const page = ref.kind === "front" ? sheet * 2 - 1 : sheet * 2;
  return page >= 0 && page < map.pages ? page : null;
}

/** Every comic page on show at a position, in reading order. */
export function visiblePages(map: BookPageMap, position: number): number[] {
  const at = clampBookPosition(map, position);
  const pages: number[] = [];

  if (map.layout === "single") {
    const page = at + 1;
    if (page >= 0 && page < map.pages) pages.push(page);
    return pages;
  }

  const first = map.hasCover ? (at < 0 ? 0 : at * 2 + 1) : at * 2;
  // Position -1 is the first page alone on the right, not a spread.
  const last = map.hasCover && at < 0 ? first : first + 1;
  for (let page = first; page <= last; page += 1) {
    if (page >= 0 && page < map.pages) pages.push(page);
  }
  return pages;
}

/**
 * Pages worth having decoded around a page: the spread it is on, the next
 * spread and the previous one. Used to decide what to preload and to pin in
 * the image cache, never to load the whole book.
 */
export function pageWindow(
  map: BookPageMap,
  pageIndex: number,
  before = 3,
  after = 3,
): number[] {
  const page = clampPage(pageIndex, map.pages);
  const ahead = page + pagesPerSheet(map.layout) + after - 1;
  const pages: number[] = [];
  for (let index = page - before; index <= ahead; index += 1) {
    if (index >= 0 && index < map.pages) pages.push(index);
  }
  return pages;
}

/** grabfold's binding for a reading direction: manga is bound on the right. */
export function bookBinding(direction: ReadingDirection): "left" | "right" {
  return direction === "rtl" ? "right" : "left";
}

/** Which way a press on a side of the page turns it. */
export function turnForSide(
  side: "left" | "right",
  direction: ReadingDirection,
): PageTurn {
  return direction === "rtl"
    ? side === "left"
      ? "next"
      : "previous"
    : side === "right"
      ? "next"
      : "previous";
}

/**
 * Which way a key turns the page. Arrow keys follow the page direction
 * (← is forward in a right-bound manga), Page Up/Down are always back/forward
 * and Space turns forward.
 */
export function turnForKey(
  key: string,
  direction: ReadingDirection,
  shiftKey = false,
): PageTurn | null {
  switch (key) {
    case "PageDown":
      return "next";
    case "PageUp":
      return "previous";
    case "ArrowLeft":
      return direction === "rtl" ? "next" : "previous";
    case "ArrowRight":
      return direction === "rtl" ? "previous" : "next";
    case " ":
      return shiftKey ? "previous" : "next";
    default:
      return null;
  }
}
