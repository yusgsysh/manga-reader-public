// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useProgressiveReveal } from "./useProgressiveReveal";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function advance(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

describe("useProgressiveReveal", () => {
  it("reveals a page one card at a time instead of all at once", () => {
    const { result } = renderHook(() => useProgressiveReveal(25));

    // The first card paints with the initial commit so the skeleton swap has
    // no blank frame; the rest follow one at a time.
    expect(result.current).toBe(1);

    advance(30);
    expect(result.current).toBe(2);

    advance(30);
    expect(result.current).toBe(3);
  });

  it("keeps the stream going while the next page is still arriving", () => {
    const { result, rerender } = renderHook(
      ({ total }: { total: number }) => useProgressiveReveal(total),
      { initialProps: { total: 25 } },
    );

    advance(200);
    const midway = result.current;
    expect(midway).toBeGreaterThan(1);
    expect(midway).toBeLessThan(25);

    // The next 25 cards arrive mid-stream: the reveal continues from where it
    // was rather than restarting or jumping to the end.
    rerender({ total: 50 });
    expect(result.current).toBe(midway);

    advance(20);
    expect(result.current).toBeGreaterThan(midway);
  });

  it("eventually reveals the whole list", () => {
    const { result } = renderHook(() => useProgressiveReveal(25));

    advance(5_000);
    expect(result.current).toBe(25);
  });

  it("starts a fresh stream when an appended page lands after the last drained", () => {
    const { result, rerender } = renderHook(
      ({ total }: { total: number }) => useProgressiveReveal(total),
      { initialProps: { total: 25 } },
    );

    advance(5_000);
    expect(result.current).toBe(25);

    rerender({ total: 50 });
    expect(result.current).toBe(25);

    advance(30);
    expect(result.current).toBe(26);
  });

  it("paints a multi-page cache restore at once on mount", () => {
    const { result } = renderHook(() => useProgressiveReveal(75));

    expect(result.current).toBe(75);
  });

  it("clamps silently when the list shrinks", () => {
    const { result, rerender } = renderHook(
      ({ total }: { total: number }) => useProgressiveReveal(total),
      { initialProps: { total: 25 } },
    );

    advance(5_000);
    expect(result.current).toBe(25);

    rerender({ total: 10 });
    expect(result.current).toBe(10);

    advance(5_000);
    expect(result.current).toBe(10);
  });

  it("skips the paced reveal when reduced motion is requested", () => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn(() => ({
        matches: true,
        media: "(prefers-reduced-motion: reduce)",
        addEventListener: () => {},
        removeEventListener: () => {},
      })),
    );

    const { result } = renderHook(() => useProgressiveReveal(25));
    expect(result.current).toBe(25);
  });
});
