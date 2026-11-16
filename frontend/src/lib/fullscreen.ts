/**
 * Native Fullscreen API helpers.
 *
 * Safari (and older Edge) only expose the vendor-prefixed variants, and the
 * standard entry can still be missing on iOS. The reader treats native
 * fullscreen as an enhancement on top of its `browserFullscreen` layout, so
 * every call here is best-effort: unsupported or rejected requests resolve to
 * false instead of throwing.
 */

interface VendorFullscreenElement extends HTMLElement {
  webkitRequestFullscreen?: () => Promise<void> | void;
  msRequestFullscreen?: () => Promise<void> | void;
}

interface VendorFullscreenDocument extends Document {
  webkitFullscreenElement?: Element | null;
  msFullscreenElement?: Element | null;
  webkitExitFullscreen?: () => Promise<void> | void;
  msExitFullscreen?: () => Promise<void> | void;
}

/** The element currently fullscreen, across standard and prefixed APIs. */
export function getFullscreenElement(): Element | null {
  if (typeof document === "undefined") return null;
  const doc = document as VendorFullscreenDocument;
  return (
    doc.fullscreenElement ??
    doc.webkitFullscreenElement ??
    doc.msFullscreenElement ??
    null
  );
}

/** Whether this browser exposes any way to fullscreen a normal element. */
export function canRequestFullscreen(element: Element | null): boolean {
  if (!element) return false;
  const el = element as VendorFullscreenElement;
  return (
    typeof el.requestFullscreen === "function" ||
    typeof el.webkitRequestFullscreen === "function" ||
    typeof el.msRequestFullscreen === "function"
  );
}

/** Ask the browser to fullscreen `element`. Resolves false when unsupported. */
export async function requestElementFullscreen(
  element: Element | null,
): Promise<boolean> {
  if (!canRequestFullscreen(element)) return false;
  const el = element as VendorFullscreenElement;
  try {
    if (typeof el.requestFullscreen === "function") {
      await el.requestFullscreen({ navigationUI: "hide" });
    } else if (typeof el.webkitRequestFullscreen === "function") {
      await el.webkitRequestFullscreen();
    } else if (typeof el.msRequestFullscreen === "function") {
      await el.msRequestFullscreen();
    } else {
      return false;
    }
    return true;
  } catch {
    // Denied by policy, missing user activation, or an unsupported context.
    return false;
  }
}

/** Leave fullscreen through whichever API the browser implements. */
export async function exitElementFullscreen(): Promise<void> {
  if (typeof document === "undefined") return;
  const doc = document as VendorFullscreenDocument;
  try {
    if (typeof doc.exitFullscreen === "function") {
      if (!doc.fullscreenElement) return;
      await doc.exitFullscreen();
    } else if (typeof doc.webkitExitFullscreen === "function") {
      if (!doc.webkitFullscreenElement) return;
      await doc.webkitExitFullscreen();
    } else if (typeof doc.msExitFullscreen === "function") {
      if (!doc.msFullscreenElement) return;
      await doc.msExitFullscreen();
    }
  } catch {
    // Already exiting, or the browser rejected the call.
  }
}

/** Fullscreen-change event names, standard first. */
export const FULLSCREEN_CHANGE_EVENTS = [
  "fullscreenchange",
  "webkitfullscreenchange",
  "MSFullscreenChange",
] as const;
