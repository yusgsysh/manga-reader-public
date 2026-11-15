/**
 * Renders a grabfold page fold on top of comimi's existing stage.
 *
 * This renderer uses grabfold's own FoldingSheet (https://github.com/javocsoft/grabfold,
 * MIT License © JavocSoft). comimi's patched PageStage marks `.comimi-stage` with
 * `data-flipping="next|previous"` and the current `.comimi-page-group` with
 * `data-spine="left|right"` during a book turn; we watch those markers, create a
 * grabfold FoldingSheet over the current cell, and drive it with grabfold's
 * foldGeometry.
 *
 * The current page group is cloned into the sheet's resting slot (the face that
 * lifts) and the turned slot is left blank, so a single-sided manga page reads
 * as paper on the back rather than a mirrored image. The clone is removed once
 * the turn ends, leaving comimi's original DOM — its images, loading states and
 * event handlers — untouched.
 *
 * The renderer also owns the *interactive* turn: a page can be taken hold of
 * anywhere and dragged, the crease tilting with the hand, and a quick flick or
 * a hover peek behave the way grabfold's own book does.
 */

import {
  FoldingSheet,
  gutterGradient,
  peekProgress,
  releaseSpeed,
  releaseTarget,
  settleDuration,
  type Sample,
  type Shading,
  type SheetFrame,
} from "./bookFold";

/** Full turn duration in ms for API/keyboard turns, matching comimi's timeout. */
const DEFAULT_DURATION = 180;

const DRAG_COMMIT_AT = 0.4;
const DRAG_THRESHOLD = 8;
const PEEK_ZONE = 72;

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

type Direction = "next" | "previous";
type Spine = "left" | "right";

interface Turn {
  direction: Direction;
  spine: Spine;
  /** The current page cell within the stage, in stage pixels. */
  rect: { left: number; top: number; width: number; height: number };
  /** The current page cell in viewport pixels, for pointer math. */
  viewport: { left: number; top: number; width: number; height: number };
  /** The page element being lifted (hidden while the sheet takes over). */
  current: HTMLElement;
  mode: "drag" | "peek";
  progress: number;
  grabY: number;
  lift: number;
  /** Pointer position when the turn began (drag or peek). */
  startX: number;
  startY: number;
  /** Progress when a drag actually started, and the x it was measured from. */
  baseProgress: number;
  baseX: number;
  /** Whether the press has crossed the drag threshold. */
  dragging: boolean;
  samples: Sample[];
}

export class BookFoldRenderer {
  private stage: HTMLElement;
  private duration: number;
  private reducedMotion: boolean;
  private observer: MutationObserver | null = null;
  private raf: number | null = null;
  private turn: Turn | null = null;
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

  /** Returns true if a drag/animation/peek is currently in progress. */
  get isActive(): boolean {
    return this.turn !== null;
  }

  /* ------------------------------------------------------------- auto turn */

  private sync(): void {
    const flipping = this.stage.dataset.flipping as Direction | undefined;
    if (!flipping) {
      this.cancel();
      return;
    }
    // A drag that already went the same way owns the sheet; comimi's own
    // `data-flipping` window is just the landing dwell before it commits.
    if (this.turn?.direction === flipping) return;

    const current = this.stage.querySelector<HTMLElement>(
      '.comimi-page-group[data-placement="current"]',
    );
    if (!current) {
      this.cancel();
      return;
    }

    const spine = current.dataset.spine as Spine | undefined;
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
    direction: Direction,
    spine: Spine,
    current: HTMLElement,
    rect: DOMRect,
    stageRect: DOMRect,
  ): void {
    this.cancel();

    this.turn = {
      direction,
      spine,
      rect: this.relativeRect(rect, stageRect),
      viewport: this.viewportRect(rect),
      current,
      mode: "drag",
      progress: 0,
      grabY: rect.height / 2,
      lift: 0,
      startX: 0,
      startY: 0,
      baseProgress: 0,
      baseX: 0,
      dragging: true,
      samples: [],
    };

    this.createSheet(current, spine);
    this.animateTo(1, this.reducedMotion ? 0 : this.duration);
  }

  /* ------------------------------------------------------------------- drag */

  /**
   * Begin an interactive drag turn from a press at `pointer` (the pointer-down
   * position). `direction` and `spine` are resolved by the caller from the
   * reading direction and the drag movement. The caller should then call
   * `drag()` on pointermove and `release()` on pointerup.
   */
  beginDrag(
    direction: Direction,
    spine: Spine,
    current: HTMLElement,
    pointer: { x: number; y: number },
  ): void {
    // Adopt an in-flight peek rather than rebuilding the sheet underneath it.
    if (this.turn?.mode === "peek" && this.turn.current === current) {
      const turn = this.turn;
      turn.mode = "drag";
      turn.direction = direction;
      turn.spine = spine;
      turn.dragging = false;
      turn.baseProgress = turn.progress;
      turn.baseX = pointer.x;
      turn.startX = pointer.x;
      turn.startY = pointer.y;
      turn.samples = [{ time: performance.now(), progress: turn.progress }];
      return;
    }

    this.cancel();

    const rect = current.getBoundingClientRect();
    const stageRect = this.stage.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return;

    this.turn = {
      direction,
      spine,
      rect: this.relativeRect(rect, stageRect),
      viewport: this.viewportRect(rect),
      current,
      mode: "drag",
      progress: 0,
      grabY: clamp(pointer.y - rect.top, 0, rect.height),
      lift: 0,
      startX: pointer.x,
      startY: pointer.y,
      baseProgress: 0,
      baseX: pointer.x,
      dragging: false,
      samples: [{ time: performance.now(), progress: 0 }],
    };

    this.createSheet(current, spine);
    this.updateFold();
  }

  /** Update an active drag from the current pointer position. */
  drag(pointer: { x: number; y: number }): void {
    const turn = this.turn;
    if (!turn || turn.mode !== "drag") return;

    if (!turn.dragging) {
      const moved = Math.abs(pointer.x - turn.startX);
      if (moved < DRAG_THRESHOLD) return;
      turn.dragging = true;
      turn.baseProgress = turn.progress;
      turn.baseX = pointer.x;
    }

    const sign = turn.spine === "left" ? -1 : 1;
    const travel = Math.max(1, turn.viewport.width * 0.9);
    turn.progress = clamp(turn.baseProgress + (sign * (pointer.x - turn.baseX)) / travel, 0, 1);
    turn.lift = pointer.y - turn.startY;
    turn.samples.push({ time: performance.now(), progress: turn.progress });
    if (turn.samples.length > 24) turn.samples.splice(0, turn.samples.length - 24);
    this.updateFold();
  }

  /**
   * Finish a drag, applying grabfold's flick physics. Resolves once the sheet
   * has settled at its target; `wentOver` tells the caller whether the turn
   * should be committed.
   */
  release(): { wentOver: boolean; done: Promise<void> } {
    const turn = this.turn;
    if (!turn || turn.mode !== "drag") {
      return { wentOver: false, done: Promise.resolve() };
    }

    // A press that never crossed the threshold is a tap, not a turn.
    if (!turn.dragging) {
      this.cancel();
      return { wentOver: false, done: Promise.resolve() };
    }

    const speed = releaseSpeed(turn.samples);
    const target = releaseTarget(turn.progress, speed, DRAG_COMMIT_AT);
    const remaining = target - turn.progress;
    const duration = this.reducedMotion
      ? 0
      : settleDuration(remaining, this.duration, speed);

    const done = new Promise<void>((resolve) => {
      this.animateTo(target, duration, resolve);
    });
    return { wentOver: target === 1, done };
  }

  /* ------------------------------------------------------------------- peek */

  /** Start a hover peek: the page edge lifts a little under the pointer. */
  beginPeek(
    direction: Direction,
    spine: Spine,
    current: HTMLElement,
    pointer: { x: number; y: number },
  ): void {
    if (this.turn?.mode === "peek" && this.turn.current === current) return;
    this.cancel();

    const rect = current.getBoundingClientRect();
    const stageRect = this.stage.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return;

    this.turn = {
      direction,
      spine,
      rect: this.relativeRect(rect, stageRect),
      viewport: this.viewportRect(rect),
      current,
      mode: "peek",
      progress: 0,
      grabY: clamp(pointer.y - rect.top, 0, rect.height),
      lift: 0,
      startX: pointer.x,
      startY: pointer.y,
      baseProgress: 0,
      baseX: pointer.x,
      dragging: false,
      samples: [],
    };

    this.createSheet(current, spine);
    this.updatePeek(pointer);
  }

  /** Update the peek to follow the pointer. */
  updatePeek(pointer: { x: number; y: number }): void {
    const turn = this.turn;
    if (!turn || turn.mode !== "peek") return;

    const zone = Math.min(PEEK_ZONE, turn.viewport.width * 0.18);
    const distance =
      turn.spine === "left"
        ? turn.viewport.left + turn.viewport.width - pointer.x
        : pointer.x - turn.viewport.left;

    turn.progress = peekProgress(distance, zone);
    turn.grabY = clamp(pointer.y - turn.viewport.top, 0, turn.viewport.height);
    // A corner lifts, pulled in towards the middle of the page: a dog-ear.
    const reach = turn.progress / 0.06;
    turn.lift = (turn.viewport.height / 2 - turn.grabY) * 0.3 * reach;
    this.updateFold();
  }

  /** Drop a peek at once. */
  endPeek(): void {
    if (this.turn?.mode !== "peek") return;
    this.cancel();
  }

  /* ---------------------------------------------------------------- drawing */

  private createSheet(current: HTMLElement, spine: Spine): void {
    this.cleanup();

    const rect = this.turn?.rect;
    if (!rect) return;

    // Hide the original page group while the sheet takes over; this avoids
    // duplicating visible content and keeps comimi's DOM untouched.
    const originalDisplay = current.style.display;
    current.style.display = "none";
    this.cleanupFns.push(() => {
      current.style.display = originalDisplay;
    });

    const sheet = new FoldingSheet(document, DEFAULT_SHADING);
    this.sheet = sheet;

    // Position the sheet over the current page cell, top included.
    sheet.place(rect.left, rect.width, rect.height);
    sheet.root.style.top = `${rect.top}px`;

    // The resting (leaf) slot shows the page being lifted. The turned slot is
    // left blank so a single-sided manga page reads as its paper back.
    const restingClone = current.cloneNode(true) as HTMLElement;
    restingClone.style.display = "";
    restingClone.style.width = "100%";
    restingClone.style.height = "100%";
    sheet.restingSlot.appendChild(restingClone);

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

  /** One frame of the fold from the current turn state. */
  private updateFold(): void {
    if (!this.turn || !this.sheet) return;

    const { rect, spine, grabY, lift, progress } = this.turn;
    const frame: SheetFrame = {
      spine,
      width: rect.width,
      height: rect.height,
      progress,
      grabY,
      lift,
    };
    this.sheet.draw(frame);
  }

  /** Eases the fold from its current progress to `target`, then calls `onDone`. */
  private animateTo(target: number, durationMs: number, onDone?: () => void): void {
    const turn = this.turn;
    if (!turn) {
      onDone?.();
      return;
    }
    this.cancelRaf();

    const from = turn.progress;
    const liftFrom = turn.lift;
    const start = performance.now();

    const step = (now: number) => {
      if (this.turn !== turn) return;
      const t = durationMs <= 0 ? 1 : Math.min(1, (now - start) / durationMs);
      const eased = easeOut(t);
      turn.progress = from + (target - from) * eased;
      // The tilt runs out as the sheet arrives, so a landed page is flat.
      turn.lift = liftFrom * (1 - eased);
      this.updateFold();

      if (t >= 1) {
        this.raf = null;
        onDone?.();
        return;
      }
      this.raf = requestAnimationFrame(step);
    };
    this.raf = requestAnimationFrame(step);
  }

  /* ---------------------------------------------------------------- teardown */

  cancel(): void {
    this.cancelRaf();
    this.turn = null;
    this.cleanup();
  }

  private cancelRaf(): void {
    if (this.raf !== null) {
      cancelAnimationFrame(this.raf);
      this.raf = null;
    }
  }

  private cleanup(): void {
    for (const fn of this.cleanupFns) fn();
    this.cleanupFns = [];
    this.sheet = null;
  }

  private relativeRect(
    rect: DOMRect,
    stageRect: DOMRect,
  ): { left: number; top: number; width: number; height: number } {
    return {
      left: rect.left - stageRect.left,
      top: rect.top - stageRect.top,
      width: rect.width,
      height: rect.height,
    };
  }

  private viewportRect(rect: DOMRect): { left: number; top: number; width: number; height: number } {
    return { left: rect.left, top: rect.top, width: rect.width, height: rect.height };
  }
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(high, Math.max(low, value));
}

function easeOut(t: number): number {
  return 1 - (1 - t) * (1 - t) * (1 - t);
}
