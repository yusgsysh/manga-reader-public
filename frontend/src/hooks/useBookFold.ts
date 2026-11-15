import { useEffect, useRef } from "react";
import { BookFoldRenderer } from "../lib/bookFoldRenderer";

/**
 * Attaches a grabfold renderer to a comimi stage inside the given shell.
 *
 * This hook uses the grabfold npm package (https://github.com/javocsoft/grabfold,
 * MIT License © JavocSoft). It watches comimi's `data-flipping` marker and
 * overlays a grabfold FoldingSheet on the current page cell, replacing comimi's
 * stock rotateY with grabfold's clip-path fold. It also supports grabfold's
 * pointer-driven drag/flick turns.
 *
 * See `frontend/THIRD-PARTY-NOTICES.md` for attribution.
 */
export function useBookFold(
  shellEl: HTMLElement | null,
  options?: { reducedMotion?: boolean },
): void {
  const rendererRef = useRef<BookFoldRenderer | null>(null);
  const dragRef = useRef<{
    pointerId: number;
    direction: "next" | "previous";
  } | null>(null);

  useEffect(() => {
    if (!shellEl) return;

    const stage = shellEl.querySelector<HTMLElement>(".comimi-stage");
    if (!stage) return;

    const renderer = new BookFoldRenderer({
      stage,
      reducedMotion: options?.reducedMotion ?? false,
    });
    rendererRef.current = renderer;

    // Pointer-driven drag turns. We listen in the capture phase so we can
    // take over edge-grabs before comimi's own swipe handling moves the
    // whole stage. We only grab when the book animation is active, the page
    // is unzoomed, and the press is close to the free edge of the current
    // page group.
    const isBookMode = () =>
      shellEl
        .querySelector(".comimi-root")
        ?.getAttribute("data-page-turn-animation") === "book";

    const freeEdgeX = (spine: "left" | "right", rect: DOMRect, x: number) =>
      spine === "left" ? rect.right - x : x - rect.left;

    const handlePointerDown = (event: PointerEvent) => {
      if (!isBookMode()) return;
      if (event.button !== 0) return;
      const target = event.target as HTMLElement | null;
      if (!target) return;

      // Ignore presses on interactive chrome.
      if (target.closest(".comimi-arrows, .comimi-controls-dock, .comimi-menu")) {
        return;
      }

      const current = stage.querySelector<HTMLElement>(
        '.comimi-page-group[data-placement="current"]',
      );
      const spine = current?.dataset.spine as "left" | "right" | undefined;
      if (!current || !spine) return;

      // Only start a fold drag near the free edge.
      const rect = current.getBoundingClientRect();
      const edgeDistance = freeEdgeX(spine, rect, event.clientX);
      if (edgeDistance < 0 || edgeDistance > Math.max(24, rect.width * 0.18)) {
        return;
      }

      const direction = spine === "left" ? "next" : "previous";

      event.preventDefault();
      event.stopPropagation();
      (event.currentTarget as HTMLElement)?.setPointerCapture?.(event.pointerId);

      renderer.beginDrag(direction, spine, current, {
        x: event.clientX,
        y: event.clientY,
      });

      dragRef.current = {
        pointerId: event.pointerId,
        direction,
      };
    };

    const handlePointerMove = (event: PointerEvent) => {
      if (dragRef.current?.pointerId !== event.pointerId) return;
      event.preventDefault();
      renderer.drag({ x: event.clientX, y: event.clientY });
    };

    const handlePointerUp = (event: PointerEvent) => {
      if (dragRef.current?.pointerId !== event.pointerId) return;
      const direction = dragRef.current.direction;
      dragRef.current = null;

      const release = renderer.release();
      if (!release) return;

      // Wait for the settle animation. If the page went over, trigger comimi's
      // arrow to commit the turn; otherwise the fold drops back on its own.
      if (release.wentOver) {
        const start = performance.now();
        const check = () => {
          if (!renderer.isActive) {
            const root = stage.closest<HTMLElement>(".comimi-root");
            const arrow = root?.querySelector<HTMLElement>(
              direction === "next"
                ? ".comimi-arrow-next"
                : ".comimi-arrow-prev",
            );
            arrow?.click();
            return;
          }
          if (performance.now() - start > 600) {
            release.finish();
            return;
          }
          requestAnimationFrame(check);
        };
        requestAnimationFrame(check);
      } else {
        release.finish();
      }
    };

    stage.addEventListener("pointerdown", handlePointerDown, true);
    stage.addEventListener("pointermove", handlePointerMove, true);
    stage.addEventListener("pointerup", handlePointerUp, true);
    stage.addEventListener("pointercancel", handlePointerUp, true);

    return () => {
      stage.removeEventListener("pointerdown", handlePointerDown, true);
      stage.removeEventListener("pointermove", handlePointerMove, true);
      stage.removeEventListener("pointerup", handlePointerUp, true);
      stage.removeEventListener("pointercancel", handlePointerUp, true);
      renderer.destroy();
      rendererRef.current = null;
    };
  }, [shellEl, options?.reducedMotion]);
}
