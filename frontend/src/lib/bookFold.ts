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
