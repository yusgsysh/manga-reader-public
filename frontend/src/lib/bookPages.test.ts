import { describe, expect, it } from "vitest";
import {
  bookBinding,
  bookPageMap,
  clampPage,
  pageForPosition,
  pageForRef,
  pageWindow,
  positionForPage,
  turnForKey,
  turnForSide,
  visiblePages,
  type BookLayout,
} from "./bookPages";

const LAYOUTS: BookLayout[] = ["single", "spread"];
const COVERS = [true, false];

function maps(pages: number) {
  return LAYOUTS.flatMap((layout) =>
    COVERS.map((hasCover) => bookPageMap(pages, layout, hasCover)),
  );
}

describe("book page map", () => {
  it("shows every comic page at its position, for any length", () => {
    for (let pages = 1; pages <= 12; pages += 1) {
      for (const map of maps(pages)) {
        for (let page = 0; page < pages; page += 1) {
          const position = positionForPage(map, page);
          expect(position).toBeGreaterThanOrEqual(map.first);
          expect(position).toBeLessThanOrEqual(map.last);
          expect(visiblePages(map, position)).toContain(page);
        }
      }
    }
  });

  it("reads each position back to the page it is anchored on", () => {
    for (let pages = 1; pages <= 12; pages += 1) {
      for (const map of maps(pages)) {
        for (let position = map.first; position <= map.last; position += 1) {
          const page = pageForPosition(map, position);
          expect(page).toBeGreaterThanOrEqual(0);
          expect(page).toBeLessThan(pages);
          expect(visiblePages(map, position)).toContain(page);
          expect(positionForPage(map, page)).toBe(position);
        }
      }
    }
  });

  it("keeps a visible comic page across a layout switch", () => {
    for (let pages = 1; pages <= 12; pages += 1) {
      for (const hasCover of COVERS) {
        const layouts = [
          bookPageMap(pages, "single", hasCover),
          bookPageMap(pages, "spread", hasCover),
        ] as const;
        for (const [from, to] of [
          [layouts[0], layouts[1]],
          [layouts[1], layouts[0]],
        ] as const) {
          for (let position = from.first; position <= from.last; position += 1) {
            for (const page of visiblePages(from, position)) {
              // Switching layout rebuilds the book at the page's position in
              // the new layout; the page must still be on show.
              const next = positionForPage(to, page);
              expect(visiblePages(to, next)).toContain(page);
            }
          }
        }
      }
    }
  });

  it("gives single layout one comic page per sheet, fronts only", () => {
    const map = bookPageMap(7, "single", true);
    expect(map.sheets).toBe(7);
    expect(map.covers).toBe("none");
    expect(map.first).toBe(-1);
    expect(map.last).toBe(5);

    for (let sheet = 0; sheet < map.sheets; sheet += 1) {
      expect(pageForRef(map, { kind: "front", sheet })).toBe(sheet);
      // The back of a single-sided comic page is blank paper, not a mirrored
      // copy of the next page.
      expect(pageForRef(map, { kind: "back", sheet })).toBeNull();
    }
    expect(pageForRef(map, { kind: "front", sheet: 7 })).toBeNull();
  });

  it("puts the first comic page alone on the right when covers are shown", () => {
    const map = bookPageMap(5, "spread", true);
    expect(map.sheets).toBe(3);
    expect(map.covers).toBe("none");
    expect(map.first).toBe(-1);
    expect(map.last).toBe(1);

    expect(visiblePages(map, -1)).toEqual([0]);
    expect(visiblePages(map, 0)).toEqual([1, 2]);
    expect(visiblePages(map, 1)).toEqual([3, 4]);

    expect(pageForRef(map, { kind: "front", sheet: 0 })).toBe(0);
    expect(pageForRef(map, { kind: "back", sheet: 0 })).toBe(1);
    expect(pageForRef(map, { kind: "back", sheet: 2 })).toBeNull();
  });

  it("pairs pages from the first spread when covers are hidden", () => {
    const map = bookPageMap(5, "spread", false);
    expect(map.sheets).toBe(3);
    expect(map.covers).toBe("inside");
    expect(map.first).toBe(0);

    expect(visiblePages(map, 0)).toEqual([0, 1]);
    expect(visiblePages(map, 1)).toEqual([2, 3]);
    // The last page is alone on the left, with the inside back cover beside it.
    expect(visiblePages(map, 2)).toEqual([4]);

    // Sheet 0's front is the blank inside cover that page 0 is printed behind.
    expect(pageForRef(map, { kind: "front", sheet: 0 })).toBeNull();
    expect(pageForRef(map, { kind: "back", sheet: 0 })).toBe(0);
    expect(pageForRef(map, { kind: "front", sheet: 1 })).toBe(1);
  });

  it("keeps the final page of an even comic reachable in a spread", () => {
    const map = bookPageMap(6, "spread", true);
    expect(visiblePages(map, map.last)).toEqual([5]);
    expect(positionForPage(map, 5)).toBe(map.last);

    const covered = bookPageMap(6, "spread", false);
    expect(visiblePages(covered, covered.last)).toEqual([4, 5]);
  });

  it("handles one page and two pages", () => {
    const one = bookPageMap(1, "spread", true);
    expect(one.sheets).toBe(1);
    expect(one.first).toBe(-1);
    expect(one.last).toBe(-1);
    expect(visiblePages(one, -1)).toEqual([0]);

    const two = bookPageMap(2, "spread", true);
    expect(two.sheets).toBe(2);
    expect(two.last).toBe(0);
    expect(visiblePages(two, 0)).toEqual([1]);

    const twoNoCover = bookPageMap(2, "spread", false);
    expect(visiblePages(twoNoCover, 0)).toEqual([0, 1]);
  });

  it("clamps pages and positions into the book", () => {
    const map = bookPageMap(4, "single", true);
    expect(clampPage(-3, 4)).toBe(0);
    expect(clampPage(99, 4)).toBe(3);
    expect(pageForPosition(map, -99)).toBe(0);
    expect(pageForPosition(map, 99)).toBe(3);
  });

  it("preloads only a window around the reading position", () => {
    const map = bookPageMap(40, "spread", true);
    const window = pageWindow(map, 10, 2, 2);
    expect(window[0]).toBe(8);
    expect(window.at(-1)).toBe(13);
    expect(window).not.toContain(14);

    const start = pageWindow(map, 0, 2, 2);
    expect(Math.min(...start)).toBe(0);

    const end = pageWindow(map, 39, 2, 2);
    expect(Math.max(...end)).toBe(39);
  });
});

describe("book direction", () => {
  it("binds manga on the right and western books on the left", () => {
    expect(bookBinding("rtl")).toBe("right");
    expect(bookBinding("ltr")).toBe("left");
  });

  it("turns forward towards the side the reader reads from", () => {
    expect(turnForSide("left", "rtl")).toBe("next");
    expect(turnForSide("right", "rtl")).toBe("previous");
    expect(turnForSide("right", "ltr")).toBe("next");
    expect(turnForSide("left", "ltr")).toBe("previous");
  });

  it("maps arrow keys to the page direction, never to the screen side", () => {
    expect(turnForKey("ArrowLeft", "rtl")).toBe("next");
    expect(turnForKey("ArrowRight", "rtl")).toBe("previous");
    expect(turnForKey("ArrowLeft", "ltr")).toBe("previous");
    expect(turnForKey("ArrowRight", "ltr")).toBe("next");
    expect(turnForKey("PageDown", "rtl")).toBe("next");
    expect(turnForKey("PageUp", "ltr")).toBe("previous");
    expect(turnForKey(" ", "rtl")).toBe("next");
    expect(turnForKey(" ", "ltr", true)).toBe("previous");
    expect(turnForKey("Enter", "rtl")).toBeNull();
  });
});
