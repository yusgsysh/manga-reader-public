import { useEffect, useRef, useState } from "react";

/*
 * Eases a list that arrives in fixed-size chunks (ExHentai listings are 25
 * galleries per page) into the DOM one card at a time, so an infinite-scroll
 * append reads as a continuous flow instead of a 25-card block appearing at
 * once.
 *
 * The network is untouched — the data is already in the query cache when this
 * runs; only mounting is paced. Cards near the end of a chunk linger a little,
 * which gives a slower next request time to land before the stream runs dry,
 * so already-loaded cards can wait without ever delaying a fetch.
 */

/** Delay between two cards while streaming (≈0.6s for a 25-card page). */
const REVEAL_INTERVAL_MS = 24;
/** Lower bound so restoring a many-page cache still finishes quickly. */
const MIN_REVEAL_INTERVAL_MS = 10;
/** Time budget used to derive the per-card interval from a chunk's size. */
const REVEAL_BUDGET_MS = 600;
/** The last cards of a chunk are spread out to bridge the next request. */
const TAIL_ITEMS = 4;
const TAIL_FACTOR = 1.7;
const TAIL_INTERVAL_CAP_MS = 96;
/**
 * A cache restore can hold several pages already; replaying the whole cascade
 * on every back navigation is noise, so a large list paints at once on mount.
 * Anything appended after mount still animates.
 */
const MAX_ANIMATED_INITIAL = 40;

function baseInterval(remaining: number): number {
  return Math.max(
    MIN_REVEAL_INTERVAL_MS,
    Math.min(REVEAL_INTERVAL_MS, REVEAL_BUDGET_MS / Math.max(remaining, 1)),
  );
}

function tailDelay(left: number): number {
  if (left > TAIL_ITEMS) return 0;
  return Math.min(
    REVEAL_INTERVAL_MS * TAIL_FACTOR ** (TAIL_ITEMS - left + 1),
    TAIL_INTERVAL_CAP_MS,
  );
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

/**
 * Returns how many items of `total` may render right now. The count only ever
 * grows by one per tick, except for silent clamps when the list shrinks.
 */
export function useProgressiveReveal(total: number): number {
  const [revealed, setRevealed] = useState(() => {
    if (prefersReducedMotion() || total > MAX_ANIMATED_INITIAL) return total;
    // The first card is part of the initial paint, so swapping the skeleton
    // out never shows an empty grid for a frame.
    return Math.min(1, total);
  });
  const revealedRef = useRef(revealed);
  const totalRef = useRef(total);

  // Mirrored into refs so the pacing effect below can read the latest values
  // without being torn down and rescheduled on every single card.
  useEffect(() => {
    revealedRef.current = revealed;
  }, [revealed]);

  useEffect(() => {
    totalRef.current = total;
    // A shorter list means the filters changed or items were deleted: clamp
    // silently instead of animating cards away.
    if (total < revealedRef.current) {
      revealedRef.current = total;
      setRevealed(total);
    }
  }, [total]);

  // One timer chain per pending stretch of cards. If data arrives while the
  // chain is still running, the chain picks the new cards up through
  // `totalRef`; once it has drained, `pending` flips and a new chain starts.
  const pending = revealed < total;

  useEffect(() => {
    if (!pending) return;

    if (prefersReducedMotion()) {
      revealedRef.current = totalRef.current;
      setRevealed(totalRef.current);
      return;
    }

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    let cursor = revealedRef.current;

    const step = () => {
      if (cancelled) return;
      cursor = Math.min(cursor + 1, totalRef.current);
      revealedRef.current = cursor;
      setRevealed(cursor);

      const left = totalRef.current - cursor;
      if (left <= 0) return;

      timer = setTimeout(step, baseInterval(left) + tailDelay(left));
    };

    timer = setTimeout(
      step,
      // An empty mount (data arrived after the grid did) also paints its first
      // card on the next frame instead of waiting out an interval.
      cursor === 0 ? 0 : baseInterval(totalRef.current - cursor),
    );
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [pending]);

  return Math.min(revealed, total);
}
