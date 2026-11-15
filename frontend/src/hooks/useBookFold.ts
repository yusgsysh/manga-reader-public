import { useEffect, useRef } from "react";
import { BookFoldRenderer } from "../lib/bookFoldRenderer";
import {
  DRAG_THRESHOLD,
  sideTurn,
  spineFor,
  turnDirection,
  type ReadingDirection,
  type TurnDirection,
} from "../lib/bookFold";

/**
 * Attaches a grabfold renderer to a comimi stage inside the given shell, and
 * drives grabfold's interactive page turn — grab anywhere, the crease tilting
 * with the hand, a hover peek and a flick — on top of comimi's single-page
 * reader. See `frontend/THIRD-PARTY-NOTICES.md` for attribution.
 *
 * It watches comimi's `data-flipping` marker for API/keyboard turns, and takes
 * over the page area's pointer events in book mode so a page folds live while
 * it is dragged, rather than comimi's stock slide-then-flip.
 *
 * comimi builds its stage after the viewer mounts, so the hook waits for the
 * stage to appear before attaching.
 */
export function useBookFold(
  shellEl: HTMLElement | null,
  options?: { reducedMotion?: boolean },
): void {
  const rendererRef = useRef<BookFoldRenderer | null>(null);
  const dragRef = useRef<{
    pointerId: number;
    startX: number;
    startY: number;
    readingDirection: ReadingDirection;
    current: HTMLElement;
    decided: boolean;
    direction: TurnDirection;
  } | null>(null);

  useEffect(() => {
    if (!shellEl) return;

    let renderer: BookFoldRenderer | null = null;
    let observer: MutationObserver | null = null;
    const cleanups: Array<() => void> = [];
    /** Set when a press became a drag, so the click it ends on is swallowed. */
    let dragged = false;

    const attach = (stage: HTMLElement) => {
      renderer = new BookFoldRenderer({
        stage,
        reducedMotion: options?.reducedMotion ?? false,
      });
      rendererRef.current = renderer;

      const isBookMode = () =>
        shellEl
          .querySelector(".comimi-root")
          ?.getAttribute("data-page-turn-animation") === "book";

      // comimi exposes the reading direction on the seek bar's data attribute.
      const readReadingDirection = (): ReadingDirection =>
        shellEl.querySelector<HTMLElement>(".comimi-seek-bar")?.dataset.direction ===
        "ltr"
          ? "ltr"
          : "rtl";

      // A zoomed page pans instead of turning; the fold stays out of the way.
      const isZoomed = (current: HTMLElement) =>
        current.style.transform.includes("scale(");

      const currentPage = (): HTMLElement | null =>
        stage.querySelector<HTMLElement>(
          '.comimi-page-group[data-placement="current"]',
        );

      const onInteractiveChrome = (target: EventTarget | null) =>
        target instanceof Element &&
        !!target.closest(".comimi-arrows, .comimi-controls-dock, .comimi-menu");

      // comimi wires the arrows by position: in rtl the left arrow advances.
      const arrowFor = (
        direction: TurnDirection,
        readingDirection: ReadingDirection,
      ): HTMLElement | null => {
        const cls =
          direction === "next"
            ? readingDirection === "rtl"
              ? "comimi-arrow-prev"
              : "comimi-arrow-next"
            : readingDirection === "rtl"
              ? "comimi-arrow-next"
              : "comimi-arrow-prev";
        return stage
          .closest<HTMLElement>(".comimi-root")
          ?.querySelector<HTMLElement>(`.${cls}`) ?? null;
      };

      const clickArrow = (
        direction: TurnDirection,
        readingDirection: ReadingDirection,
      ) => {
        arrowFor(direction, readingDirection)?.click();
      };

      // comimi marks an arrow `data-disabled` when there is no page that way.
      const canTurn = (
        direction: TurnDirection,
        readingDirection: ReadingDirection,
      ) => arrowFor(direction, readingDirection)?.dataset.disabled !== "true";

      // ---- Drag -----------------------------------------------------------

      const handlePointerDown = (event: PointerEvent) => {
        dragged = false;
        if (!isBookMode()) return;
        if (event.pointerType === "mouse" && event.button !== 0) return;
        if (onInteractiveChrome(event.target)) return;
        // A turn already in flight (from a key or an arrow) owns the stage.
        if (stage.dataset.flipping) return;

        const current = currentPage();
        if (!current || isZoomed(current)) return;

        // Suppress comimi's mouse handling (slide preview) so the fold can
        // follow the pointer live. Taps keep working through comimi's own
        // click-to-turn, whose click is not a suppressible mouse event.
        event.preventDefault();
        try {
          (event.currentTarget as HTMLElement)?.setPointerCapture?.(event.pointerId);
        } catch {
          // The drag still tracks while the pointer is over the book.
        }

        dragRef.current = {
          pointerId: event.pointerId,
          startX: event.clientX,
          startY: event.clientY,
          readingDirection: readReadingDirection(),
          current,
          decided: false,
          direction: "next",
        };
      };

      const handlePointerMove = (event: PointerEvent) => {
        const drag = dragRef.current;
        if (!drag || drag.pointerId !== event.pointerId) {
          // No active drag: a mouse hovering near a free edge lifts the page.
          if (
            !drag &&
            event.pointerType === "mouse" &&
            event.buttons === 0 &&
            isBookMode() &&
            !stage.dataset.flipping &&
            !onInteractiveChrome(event.target)
          ) {
            updatePeek(event.clientX, event.clientY);
          }
          return;
        }
        event.preventDefault();

        if (!drag.decided) {
          const dx = event.clientX - drag.startX;
          if (Math.abs(dx) < DRAG_THRESHOLD) return;
          drag.decided = true;
          dragged = true;
          drag.direction = turnDirection(dx, drag.readingDirection);
          // Nowhere to turn: drop the press instead of folding into nothing.
          if (!canTurn(drag.direction, drag.readingDirection)) {
            dragRef.current = null;
            return;
          }
          renderer?.beginDrag(
            drag.direction,
            spineFor(drag.direction, drag.readingDirection),
            drag.current,
            { x: drag.startX, y: drag.startY },
          );
        }

        renderer?.drag({ x: event.clientX, y: event.clientY });
      };

      const handlePointerUp = (event: PointerEvent) => {
        const drag = dragRef.current;
        const active = renderer;
        if (!drag || drag.pointerId !== event.pointerId) return;
        dragRef.current = null;
        if (!drag.decided || !active) return;

        const release = active.release();
        void release.done.then(() => {
          if (release.wentOver) clickArrow(drag.direction, drag.readingDirection);
          else active.cancel();
        });
      };

      const handlePointerCancel = (event: PointerEvent) => {
        if (dragRef.current?.pointerId === event.pointerId) {
          dragRef.current = null;
          renderer?.cancel();
        }
      };

      // ---- Click -----------------------------------------------------------

      // A drag ends on a click, and that click must not reach comimi's
      // click-to-turn. Caught on the way down, before comimi's own handler.
      const swallowClickAfterDrag = (event: MouseEvent) => {
        if (!dragged) return;
        dragged = false;
        event.preventDefault();
        event.stopPropagation();
      };

      // ---- Peek -----------------------------------------------------------

      const updatePeek = (x: number, y: number) => {
        const current = currentPage();
        if (!current || isZoomed(current)) {
          renderer?.endPeek();
          return;
        }
        const readingDirection = readReadingDirection();
        const rect = current.getBoundingClientRect();
        const zone = Math.min(72, rect.width * 0.18);

        let side: "left" | "right" | null = null;
        if (x < rect.left + zone) side = readingDirection === "rtl" ? "left" : "right";
        else if (x > rect.right - zone) side = readingDirection === "rtl" ? "right" : "left";

        if (!side) {
          renderer?.endPeek();
          return;
        }

        const direction = sideTurn(side, readingDirection);
        if (!canTurn(direction, readingDirection)) {
          renderer?.endPeek();
          return;
        }
        renderer?.beginPeek(direction, spineFor(direction, readingDirection), current, {
          x,
          y,
        });
        renderer?.updatePeek({ x, y });
      };

      const handlePointerLeave = (event: PointerEvent) => {
        if (event.pointerType !== "mouse") return;
        renderer?.endPeek();
      };

      stage.addEventListener("pointerdown", handlePointerDown, true);
      stage.addEventListener("pointermove", handlePointerMove, true);
      stage.addEventListener("pointerup", handlePointerUp, true);
      stage.addEventListener("pointercancel", handlePointerCancel, true);
      stage.addEventListener("pointerleave", handlePointerLeave, true);
      stage.addEventListener("click", swallowClickAfterDrag, true);

      cleanups.push(() => {
        stage.removeEventListener("pointerdown", handlePointerDown, true);
        stage.removeEventListener("pointermove", handlePointerMove, true);
        stage.removeEventListener("pointerup", handlePointerUp, true);
        stage.removeEventListener("pointercancel", handlePointerCancel, true);
        stage.removeEventListener("pointerleave", handlePointerLeave, true);
        stage.removeEventListener("click", swallowClickAfterDrag, true);
      });
    };

    const existing = shellEl.querySelector<HTMLElement>(".comimi-stage");
    if (existing) {
      attach(existing);
    } else {
      observer = new MutationObserver(() => {
        const found = shellEl.querySelector<HTMLElement>(".comimi-stage");
        if (!found) return;
        observer?.disconnect();
        observer = null;
        attach(found);
      });
      observer.observe(shellEl, { childList: true, subtree: true });
    }

    return () => {
      observer?.disconnect();
      for (const fn of cleanups) fn();
      renderer?.destroy();
      rendererRef.current = null;
    };
  }, [shellEl, options?.reducedMotion]);
}
