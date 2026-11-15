/**
 * Grabfold adapter for comimi.
 *
 * Re-exports the geometry, physics and rendering types/functions used by the
 * reader from the grabfold package (https://github.com/javocsoft/grabfold,
 * MIT License © JavocSoft), plus a small local `polygon` helper that grabfold
 * keeps internal.
 */

export {
  clipHalfPlane,
  foldDepth,
  foldGeometry,
  reflect,
} from "grabfold";
export type { Fold, FoldInput, Point, Strip } from "grabfold";

export {
  FLICK_SPEED,
  peekProgress,
  releaseSpeed,
  releaseTarget,
  settleDuration,
} from "grabfold";
export type { Sample } from "grabfold";

export { FoldingSheet, gutterGradient } from "grabfold";
export type { Shading, SheetFrame, SheetRenderer } from "grabfold";

/** Builds a CSS `polygon(...)` string from pixel points. */
export function polygon(points: readonly { x: number; y: number }[]): string {
  if (points.length < 3) return "polygon(0 0, 0 0, 0 0)";
  return `polygon(${points
    .map((point) => `${point.x.toFixed(2)}px ${point.y.toFixed(2)}px`)
    .join(", ")})`;
}

/* ------------------------------------------------------------------ turn */

/** Which way the book is read. comimi exposes this as `"rtl" | "ltr"`. */
export type ReadingDirection = "rtl" | "ltr";

/** A single-page step, in comimi's vocabulary (`data-flipping` uses these). */
export type TurnDirection = "next" | "previous";

/** How far a page lifts when the pointer hovers near its free edge. */
export const PEEK_MAX = 0.06;

/** The widest a peek's zone gets, in pixels, however large the page. */
export const PEEK_ZONE = 72;

/** Sideways pixels before a press becomes a drag. */
export const DRAG_THRESHOLD = 8;

/** Past this share of the way over, letting go finishes the turn. */
export const DRAG_COMMIT_AT = 0.4;

/**
 * Which way a horizontal movement turns the page.
 *
 * Mirrors comimi's own swipe mapping: in rtl (manga) a drag to the right is
 * "next", in ltr a drag to the left is "next".
 */
export function turnDirection(dx: number, readingDirection: ReadingDirection): TurnDirection {
  return readingDirection === "rtl"
    ? dx > 0
      ? "next"
      : "previous"
    : dx < 0
      ? "next"
      : "previous";
}

/**
 * Which cell edge the turning page is bound on, matching the mapping comimi's
 * patched `beginBookFlip` uses to set `data-spine`.
 */
export function spineFor(direction: TurnDirection, readingDirection: ReadingDirection): "left" | "right" {
  return direction === "next"
    ? readingDirection === "rtl"
      ? "right"
      : "left"
    : readingDirection === "rtl"
      ? "left"
      : "right";
}

/**
 * The sign that turns a horizontal drag into fold progress: for a page bound
 * on the left the fold travels left, for one bound on the right it travels
 * right.
 */
export function foldSign(spine: "left" | "right"): 1 | -1 {
  return spine === "left" ? -1 : 1;
}

/**
 * Which way a press on a given side of the page turns it. Matches comimi's
 * click-to-turn: in rtl the left half is "next", in ltr the right half is.
 */
export function sideTurn(side: "left" | "right", readingDirection: ReadingDirection): TurnDirection {
  return readingDirection === "rtl"
    ? side === "left"
      ? "next"
      : "previous"
    : side === "right"
      ? "next"
      : "previous";
}
