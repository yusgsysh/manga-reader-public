/**
 * Renders a grabfold page fold on top of comimi's existing stage.
 *
 * This renderer uses grabfold's own FoldingSheet (https://github.com/javocsoft/grabfold,
 * MIT License © JavocSoft) instead of a hand-rolled DOM structure. comimi's patched
 * PageStage marks `.comimi-stage` with `data-flipping="next|previous"` and the
 * current `.comimi-page-group` with `data-spine="left|right"` during a book turn;
 * we watch those markers, create a grabfold FoldingSheet over the current cell,
 * and drive it with grabfold's foldGeometry.
 *
 * The current page group is cloned into the sheet's resting and turned slots so
 * that comimi's original DOM is left untouched: its images, loading states and
 * event handlers stay in place, and the clones are removed once the turn ends.
 */

import {
  FoldingSheet,
  gutterGradient,
  releaseSpeed,
  releaseTarget,
  settleDuration,
  type Sample,
  type Shading,
  type SheetFrame,
} from "./bookFold";

const DEFAULT_DURATION = 180;
const DRAG_COMMIT_AT = 0.4;

/** Minimum horizontal movement, in pixels, before a press becomes a drag. */
const DRAG_THRESHOLD = 8;

/** Default shading values matching grabfold's defaults. */
const DEFAULT_SHADING: Shading = {
  shadow: 0.4,
  gutter: 0.3,
  lift: 45,
};

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
  private sheet: FoldingSheet | null = null;
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

    this.createSheet(current);
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

    this.createSheet(current);
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
      // Keep the sheet visible until comimi removes data-flipping; that way the
      // fold does not vanish before the page swap commits.
      this.raf = null;
      return;
    }

    this.raf = requestAnimationFrame((next) => this.tick(next));
  }

  private updateFold(progress: number): void {
    if (!this.turn || !this.sheet) return;

    const { rect, spine, grabY, lift } = this.turn;
    const width = rect.width;
    const height = rect.height;

    const frame: SheetFrame = { spine, width, height, progress, grabY, lift };
    this.sheet.draw(frame);
  }

  private createSheet(current: HTMLElement): void {
    this.cleanup();

    const rect = this.turn?.rect;
    const spine = this.turn?.spine;
    if (!rect || !spine) return;

    // Hide the original page group while the sheet takes over; this avoids
    // duplicating visible content and keeps comimi's DOM untouched.
    const originalDisplay = current.style.display;
    current.style.display = "none";
    this.cleanupFns.push(() => {
      current.style.display = originalDisplay;
    });

    const sheet = new FoldingSheet(document, DEFAULT_SHADING);
    this.sheet = sheet;

    // Position the sheet over the current page cell.
    sheet.place(rect.left, rect.width, rect.height);

    // Fill the resting (leaf) slot with a clone of the current page.
    const restingClone = current.cloneNode(true) as HTMLElement;
    restingClone.style.display = "";
    restingClone.style.width = "100%";
    restingClone.style.height = "100%";
    sheet.restingSlot.appendChild(restingClone);

    // Fill the turned (flap) slot with a mirrored clone so the single-sided
    // page reads as the paper back. The mirroring is applied via CSS on the
    // grabfold slot.
    const turnedClone = current.cloneNode(true) as HTMLElement;
    turnedClone.style.display = "";
    turnedClone.style.width = "100%";
    turnedClone.style.height = "100%";
    sheet.turnedSlot.appendChild(turnedClone);

    // Set the gutter shading to match the spine side.
    const gutter = sheet.root.querySelector(".grabfold-gutter") as HTMLElement | null;
    if (gutter) {
      gutter.style.backgroundImage = gutterGradient(spine, DEFAULT_SHADING.gutter);
    }

    this.stage.appendChild(sheet.root);
    sheet.show();

    this.cleanupFns.push(() => {
      sheet.hide();
      sheet.root.remove();
    });
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
    this.sheet = null;
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
