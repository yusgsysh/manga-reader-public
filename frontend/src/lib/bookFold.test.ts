import { describe, expect, it } from "vitest";
import {
  clipHalfPlane,
  foldDepth,
  foldGeometry,
  foldSign,
  peekProgress,
  polygon,
  reflect,
  releaseSpeed,
  releaseTarget,
  settleDuration,
  sideTurn,
  spineFor,
  turnDirection,
} from "./bookFold";

/**
 * These tests act as integration smoke tests for the grabfold adapter used by
 * comimi. The underlying geometry and physics are covered by grabfold's own
 * unit tests; here we only verify that the re-exported API behaves as the
 * reader expects.
 */

describe("bookFold geometry adapter", () => {
  it("clips a rectangle with a half-plane", () => {
    const square = [
      { x: 0, y: 0 },
      { x: 100, y: 0 },
      { x: 100, y: 100 },
      { x: 0, y: 100 },
    ];
    const clipped = clipHalfPlane(square, (p) => p.x - 50);
    expect(clipped.length).toBeGreaterThanOrEqual(3);
    expect(clipped.every((p) => p.x >= 49.9)).toBe(true);
  });

  it("reflects a point across a line", () => {
    const reflected = reflect({ x: 100, y: 0 }, { x: 50, y: 0 }, { x: 1, y: 0 });
    expect(reflected.x).toBeCloseTo(0);
    expect(reflected.y).toBeCloseTo(0);
  });

  it("produces a valid polygon string", () => {
    expect(
      polygon([
        { x: 0, y: 0 },
        { x: 10, y: 0 },
        { x: 10, y: 10 },
      ]),
    ).toBe("polygon(0.00px 0.00px, 10.00px 0.00px, 10.00px 10.00px)");
    expect(polygon([])).toBe("polygon(0 0, 0 0, 0 0)");
  });

  it("folds a left-bound page flat at progress 0", () => {
    const fold = foldGeometry({
      spine: "left",
      width: 100,
      height: 200,
      progress: 0,
      grabY: 100,
      lift: 0,
    });
    expect(fold.leaf.length).toBeGreaterThanOrEqual(3);
    expect(fold.flap.length).toBeGreaterThanOrEqual(3);
    expect(foldDepth(0)).toBeCloseTo(0);
  });

  it("folds a right-bound page fully at progress 1", () => {
    const fold = foldGeometry({
      spine: "right",
      width: 100,
      height: 200,
      progress: 1,
      grabY: 50,
      lift: 0,
    });
    expect(fold.leaf.length).toBeGreaterThanOrEqual(3);
    expect(fold.flap.length).toBeGreaterThanOrEqual(3);
    expect(foldDepth(1)).toBeCloseTo(0);
    expect(foldDepth(0.5)).toBeCloseTo(1, 1);
  });
});

describe("bookFold physics adapter", () => {
  it("detects a flick from recent samples", () => {
    const samples = [
      { time: 0, progress: 0 },
      { time: 50, progress: 0.05 },
    ];
    expect(releaseSpeed(samples)).toBe(0.001);
  });

  it("lets a flick decide the target even from a short pull", () => {
    expect(releaseTarget(0.05, 0.001, 0.4)).toBe(1);
  });

  it("falls back to commit distance when no flick", () => {
    expect(releaseTarget(0.5, 0, 0.4)).toBe(1);
    expect(releaseTarget(0.2, 0, 0.4)).toBe(0);
  });

  it("settles quickly after a fast flick", () => {
    const duration = settleDuration(0.5, 300, 0.001);
    expect(duration).toBeLessThan(300);
    expect(duration).toBeGreaterThanOrEqual(120);
  });

  it("peeks more near the edge", () => {
    expect(peekProgress(0, 40)).toBeGreaterThan(peekProgress(20, 40));
    expect(peekProgress(50, 40)).toBe(0);
  });
});

describe("bookFold direction mapping", () => {
  it("turns a drag the way comimi does", () => {
    expect(turnDirection(-10, "rtl")).toBe("previous");
    expect(turnDirection(10, "rtl")).toBe("next");
    expect(turnDirection(-10, "ltr")).toBe("next");
    expect(turnDirection(10, "ltr")).toBe("previous");
  });

  it("picks the spine the same way as comimi's book flip", () => {
    expect(spineFor("next", "rtl")).toBe("right");
    expect(spineFor("previous", "rtl")).toBe("left");
    expect(spineFor("next", "ltr")).toBe("left");
    expect(spineFor("previous", "ltr")).toBe("right");
  });

  it("folds left-bound pages against the drag, right-bound pages with it", () => {
    expect(foldSign("left")).toBe(-1);
    expect(foldSign("right")).toBe(1);
  });

  it("maps a side press to a turn", () => {
    expect(sideTurn("left", "rtl")).toBe("next");
    expect(sideTurn("right", "rtl")).toBe("previous");
    expect(sideTurn("right", "ltr")).toBe("next");
    expect(sideTurn("left", "ltr")).toBe("previous");
  });
});
