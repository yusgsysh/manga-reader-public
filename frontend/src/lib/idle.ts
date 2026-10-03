// Lets heavy main-thread work (tag-DB inflate/parse, index build) wait for an
// idle slice instead of landing in the middle of a route transition or scroll.
// `timeout` guarantees progress even on devices that stay busy.

const IDLE_TIMEOUT_MS = 4000;

export function nextIdle(timeout: number = IDLE_TIMEOUT_MS): Promise<void> {
  return new Promise((resolve) => {
    if (typeof window === "undefined") {
      resolve();
      return;
    }
    if (typeof window.requestIdleCallback === "function") {
      window.requestIdleCallback(() => resolve(), { timeout });
      return;
    }
    window.setTimeout(resolve, 250);
  });
}

/** Schedules `fn` for the next idle slice and returns a cancel function. */
export function scheduleIdle(
  fn: () => void,
  timeout: number = IDLE_TIMEOUT_MS,
): () => void {
  if (typeof window === "undefined") return () => {};
  if (typeof window.requestIdleCallback === "function") {
    const handle = window.requestIdleCallback(fn, { timeout });
    return () => window.cancelIdleCallback(handle);
  }
  const timer = window.setTimeout(fn, 250);
  return () => window.clearTimeout(timer);
}
