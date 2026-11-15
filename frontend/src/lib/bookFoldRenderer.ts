/**
 * Renders a grabfold-style page fold on top of comimi's existing stage.
 *
 * comimi's patched PageStage already marks `.comimi-stage` with
 * `data-flipping="next|previous"` and the current `.comimi-page-group` with
 * `data-spine="left|right"` during a book turn. This renderer watches those
 * markers and replaces the stock CSS rotateY with a dynamic clip-path fold:
 * the flat part of the page stays in place, the folded part is carried across
 * the crease by a reflected transform, and shading bands follow the crease.
 *
 * No DOM inside the page group is modified; a short-lived clone is used for
 * the folded flap so the page's own images, loading states and event handlers
 * stay untouched.
 */

import {
  foldDepth,
  foldGeometry,
  polygon,
  releaseSpeed,
  releaseTarget,
  settleDuration,
  type Sample,
  type Strip,
} from "./bookFold";

const DEFAULT_DURATION = 180;
const DRAG_COMMIT_AT = 0.4;

/** Minimum horizontal movement, in pixels, before a press becomes a drag. */
const DRAG_THRESHOLD = 8;

export interface BookFoldRendererOptions {
  /** The comimi stage element to attach to. */
  stage: HTMLElement;
  /** Full turn duration in ms for API/keyboard turns. */
  duration?: number;
  /** True to disable animation and finish instantly. */
  reducedMotion?: boolean;
}

interface TurnState {
  direction: "next" | "previous";
  spine: "left" | "right";
  rect: DOMRect;
  start: number;
  fromProgress: number;
  toProgress: number;
  duration: number;
  grabY: number;
  lift: number;
  samples: Sample[];
  /** The pointer x at drag start, used to filter accidental presses. */
  startX: number;
  dragging: boolean;
}

export class BookFoldRenderer {
  private stage: HTMLElement;
  private duration: number;
  private reducedMotion: boolean;
  private observer: MutationObserver | null = null;
  private raf: number | null = null;
  private turn: TurnState | null = null;
  private leaf: HTMLElement | null = null;
  private flap: HTMLElement | null = null;
  private castBand: HTMLElement | null = null;
  private rollBand: HTMLElement | null = null;
  private gutter: HTMLElement | null = null;
  private cleanupFns: Array<() => void> = [];

  constructor(options: BookFoldRendererOptions) {
    this.stage = options.stage;
    this.duration = options.duration ?? DEFAULT_DURATION;
    this.reducedMotion = options.reducedMotion ?? false;

    this.observer = new MutationObserver(() => this.sync());
    this.observer.observe(this.stage, {
      attributes: true,
      attributeFilter: ["data-flipping"],
    });
    this.sync();
  }

  /** Call when the stage may have been replaced or is no longer needed. */
  destroy(): void {
    this.cancel();
    this.observer?.disconnect();
    this.observer = null;
    this.cleanup();
  }

  /** Update reduced-motion preference at runtime. */
  setReducedMotion(reduced: boolean): void {
    this.reducedMotion = reduced;
  }

  /** Returns true if a drag/animation is currently in progress. */
  get isActive(): boolean {
    return this.turn !== null;
  }

  private sync(): void {
    const flipping = this.stage.dataset.flipping as
      | "next"
      | "previous"
      | undefined;
    if (!flipping) {
      this.cancel();
      return;
    }
    if (this.turn?.direction === flipping) return;

    const current = this.stage.querySelector<HTMLElement>(
      '.comimi-page-group[data-placement="current"]',
    );
    if (!current) {
      this.cancel();
      return;
    }

    const spine = current.dataset.spine as "left" | "right" | undefined;
    if (!spine) {
      this.cancel();
      return;
    }

    const rect = current.getBoundingClientRect();
    const stageRect = this.stage.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) {
      this.cancel();
      return;
    }

    this.startAutoTurn(flipping, spine, current, rect, stageRect);
  }

  private startAutoTurn(
    direction: "next" | "previous",
    spine: "left" | "right",
    current: HTMLElement,
    rect: DOMRect,
    stageRect: DOMRect,
  ): void {
    this.cancel();

    const height = rect.height;
    const grabY = height / 2;
    const lift = 0;

    this.turn = {
      direction,
      spine,
      rect: this.relativeRect(rect, stageRect),
      start: performance.now(),
      fromProgress: 0,
      toProgress: 1,
      duration: this.reducedMotion ? 0 : this.duration,
      grabY,
      lift,
      samples: [],
      startX: 0,
      dragging: true,
    };

    this.createLayers(current);
    this.raf = requestAnimationFrame((now) => this.tick(now));
  }

  /**
   * Begin an interactive drag turn. The caller should call `drag()` on
   * pointermove and `release()` on pointerup.
   */
  beginDrag(
    direction: "next" | "previous",
    spine: "left" | "right",
    current: HTMLElement,
    pointer: { x: number; y: number },
  ): void {
    this.cancel();

    const rect = current.getBoundingClientRect();
    const stageRect = this.stage.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return;

    const rel = this.relativeRect(rect, stageRect);
    const grabY = clamp(pointer.y - rect.top, 0, rect.height);

    this.turn = {
      direction,
      spine,
      rect: rel,
      start: performance.now(),
      fromProgress: 0,
      toProgress: 0,
      duration: 0,
      grabY,
      lift: 0,
      samples: [{ time: performance.now(), progress: 0 }],
      startX: pointer.x,
      dragging: false,
    };

    this.createLayers(current);
    this.updateFold(0);
  }

  /** Update an active drag. */
  drag(pointer: { x: number; y: number }): void {
    if (!this.turn) return;

    const { rect, spine, startX } = this.turn;
    const width = rect.width;

    if (!this.turn.dragging) {
      const moved = Math.abs(pointer.x - startX);
      if (moved < DRAG_THRESHOLD) return;
      this.turn.dragging = true;
    }

    const rawX =
      spine === "left" ? pointer.x - rect.left : rect.right - pointer.x;
    const progress = clamp(rawX / width, 0, 1);
    const lift = pointer.y - rect.top - this.turn.grabY;

    this.turn.lift = lift;
    this.turn.toProgress = progress;
    this.turn.fromProgress = progress;
    this.turn.samples.push({ time: performance.now(), progress });
    this.updateFold(progress);
  }

  /** Finish a drag, applying flick physics if appropriate. */
  release(): { finish: () => void; wentOver: boolean } | null {
    if (!this.turn) return { finish: () => {}, wentOver: false };

    // Treat a press that never crossed the drag threshold as a normal click.
    if (!this.turn.dragging) {
      this.cancel();
      return { finish: () => {}, wentOver: false };
    }

    const speed = releaseSpeed(this.turn.samples);
    const target = releaseTarget(this.turn.toProgress, speed, DRAG_COMMIT_AT);
    const remaining = target - this.turn.toProgress;

    this.turn.fromProgress = this.turn.toProgress;
    this.turn.toProgress = target;
    this.turn.start = performance.now();
    this.turn.duration = this.reducedMotion
      ? 0
      : settleDuration(remaining, this.duration, speed);
    this.turn.samples = [];

    this.raf ??= requestAnimationFrame((now) => this.tick(now));

    return {
      finish: () => {
        this.cancel();
      },
      wentOver: target === 1,
    };
  }

  private tick(now: number): void {
    if (!this.turn) return;

    const { start, duration: dur, fromProgress, toProgress } = this.turn;
    let t = dur <= 0 ? 1 : (now - start) / dur;
    t = clamp(t, 0, 1);

    const progress = fromProgress + (toProgress - fromProgress) * easeOut(t);
    this.updateFold(progress);

    if (t >= 1) {
      // Keep layers visible until comimi removes data-flipping; that way the
      // fold does not vanish before the page swap commits.
      this.raf = null;
      return;
    }

    this.raf = requestAnimationFrame((next) => this.tick(next));
  }

  private updateFold(progress: number): void {
    if (!this.turn || !this.leaf || !this.flap) return;

    const { rect, spine, grabY, lift } = this.turn;
    const width = rect.width;
    const height = rect.height;

    const fold = foldGeometry({ spine, width, height, progress, grabY, lift });
    const depth = foldDepth(progress);

    this.leaf.style.clipPath = polygon(fold.leaf);

    // The flap lives in the folded face's own mirrored coordinates, so we set
    // its size to the full cell and clip it to the folded polygon. The place
    // transform then carries it across the crease.
    this.flap.style.clipPath = polygon(fold.flap);
    this.flap.style.transform = `translate(${fold.place.x.toFixed(
      2,
    )}px, ${fold.place.y.toFixed(2)}px) rotate(${fold.place.angle.toFixed(4)}rad)`;

    // Lighten the flap as it lifts off the page.
    const brightness = 1 + 0.12 * depth;
    this.flap.style.filter = `brightness(${brightness.toFixed(3)})`;

    this.placeBand(
      this.castBand,
      fold.cast,
      spine === "left" ? "right" : "left",
      depth * 0.45,
    );
    this.placeBand(
      this.rollBand,
      fold.roll,
      spine === "left" ? "right" : "left",
      depth * 0.55,
    );

    // Gutter shadow on the leaf side, mirroring the spine edge.
    if (this.gutter) {
      const towards = spine === "left" ? "to right" : "to left";
      const strength = 0.25 * depth;
      this.gutter.style.backgroundImage =
        strength <= 0
          ? "none"
          : `linear-gradient(${towards}, rgba(0,0,0,${strength.toFixed(
              3,
            )}), rgba(0,0,0,${(strength * 0.34).toFixed(3)}) 8%, rgba(0,0,0,0) 24%)`;
    }
  }

  private placeBand(
    band: HTMLElement | null,
    strip: Strip,
    direction: "right" | "left",
    strength: number,
  ): void {
    if (!band) return;
    band.style.width = `${strip.span.toFixed(2)}px`;
    band.style.height = `${strip.length.toFixed(2)}px`;
    band.style.transform = `translate(${strip.x.toFixed(
      2,
    )}px, ${strip.y.toFixed(2)}px) rotate(${strip.angle.toFixed(4)}rad)`;

    const dark = Math.min(1, Math.max(0, strength));
    if (direction === "right") {
      band.style.backgroundImage = `linear-gradient(to right, rgba(0,0,0,${dark.toFixed(
        3,
      )}), rgba(0,0,0,0))`;
    } else {
      band.style.backgroundImage = `linear-gradient(to left, rgba(0,0,0,${dark.toFixed(
        3,
      )}), rgba(0,0,0,0))`;
    }
  }

  private createLayers(current: HTMLElement): void {
    this.cleanup();

    const rect = this.turn?.rect;
    if (!rect) return;

    const clone = current.cloneNode(true) as HTMLElement;

    // Leaf: the original current group, clipped to the flat part.
    this.leaf = current;
    current.classList.add("comimi-book-fold-leaf");
    current.style.clipPath = "polygon(0 0, 100% 0, 100% 100%, 0 100%)";
    current.style.willChange = "clip-path";
    current.style.position = "relative";
    current.style.zIndex = "3";

    // Gutter overlay on the leaf.
    this.gutter = document.createElement("div");
    this.gutter.className = "comimi-book-fold-gutter";
    this.gutter.style.cssText =
      "position:absolute;inset:0;pointer-events:none;z-index:4;";
    current.appendChild(this.gutter);
    this.cleanupFns.push(() => this.gutter?.remove());

    // Flap: a clone of the current group, clipped and transformed. We wrap it
    // in a sized container so the clone's percentage width/height resolve to
    // the page cell rather than the whole stage.
    const flapWrap = document.createElement("div");
    flapWrap.className = "comimi-book-fold-flap-wrap";
    flapWrap.style.cssText =
      "position:absolute;left:0;top:0;width:100%;height:100%;pointer-events:none;z-index:5;overflow:hidden;";
    flapWrap.style.left = `${rect.left}px`;
    flapWrap.style.top = `${rect.top}px`;
    flapWrap.style.width = `${rect.width}px`;
    flapWrap.style.height = `${rect.height}px`;

    this.flap = clone;
    clone.classList.add("comimi-book-fold-flap");
    clone.style.cssText =
      "position:absolute;left:0;top:0;width:100%;height:100%;will-change:transform,clip-path;";
    // Mirror the content horizontally so the folded face reads correctly
    // after the turn (the page image is single-sided, so this simulates the
    // paper back).
    clone.style.transform = "scaleX(-1)";
    flapWrap.appendChild(clone);
    this.stage.appendChild(flapWrap);
    this.cleanupFns.push(() => flapWrap.remove());

    // Shading bands, also scoped to the page cell.
    const bandWrap = document.createElement("div");
    bandWrap.className = "comimi-book-fold-band-wrap";
    bandWrap.style.cssText =
      "position:absolute;left:0;top:0;pointer-events:none;z-index:6;overflow:visible;";
    bandWrap.style.left = `${rect.left}px`;
    bandWrap.style.top = `${rect.top}px`;
    bandWrap.style.width = `${rect.width}px`;
    bandWrap.style.height = `${rect.height}px`;

    this.castBand = this.createBand("comimi-book-fold-cast");
    this.rollBand = this.createBand("comimi-book-fold-roll");
    bandWrap.appendChild(this.castBand);
    bandWrap.appendChild(this.rollBand);
    this.stage.appendChild(bandWrap);
    this.cleanupFns.push(() => bandWrap.remove());
  }

  private createBand(className: string): HTMLElement {
    const band = document.createElement("div");
    band.className = className;
    band.style.cssText =
      "position:absolute;left:0;top:0;pointer-events:none;z-index:6;transform-origin:0 0;";
    return band;
  }

  private cancel(): void {
    if (this.raf !== null) {
      cancelAnimationFrame(this.raf);
      this.raf = null;
    }
    this.turn = null;
    this.cleanup();
  }

  private cleanup(): void {
    for (const fn of this.cleanupFns) fn();
    this.cleanupFns = [];

    if (this.leaf) {
      this.leaf.classList.remove("comimi-book-fold-leaf");
      this.leaf.style.clipPath = "";
      this.leaf.style.willChange = "";
      this.leaf.style.position = "";
      this.leaf.style.zIndex = "";
      this.leaf = null;
    }

    this.flap = null;
    this.castBand = null;
    this.rollBand = null;
    this.gutter = null;
  }

  private relativeRect(rect: DOMRect, stageRect: DOMRect): DOMRect {
    return new DOMRect(
      rect.left - stageRect.left,
      rect.top - stageRect.top,
      rect.width,
      rect.height,
    );
  }
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(high, Math.max(low, value));
}

function easeOut(t: number): number {
  // Smooth deceleration matching the stock spring feel.
  return 1 - (1 - t) * (1 - t);
}
