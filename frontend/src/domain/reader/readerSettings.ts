/**
 * The reader settings the app chrome shares with comimi.
 *
 * comimi owns its full settings store, but the reader top bar mirrors this
 * subset so it can draw the current direction, page mode and autoplay interval
 * without holding a second copy of the reading position.
 */

import type { ViewerSettings } from "@yui540/comimi-react";

export interface ReaderSettings {
  readingDirection: "rtl" | "ltr";
  pageTurnMode: "single" | "spread";
  hasCover: boolean;
  autoPageTurnIntervalMs: number;
}

export const DEFAULT_READER_SETTINGS: ReaderSettings = Object.freeze({
  readingDirection: "rtl",
  pageTurnMode: "single",
  hasCover: true,
  autoPageTurnIntervalMs: 5000,
});

export function pickReaderSettings(settings: ViewerSettings): ReaderSettings {
  return {
    readingDirection: settings.readingDirection,
    pageTurnMode: settings.pageTurnMode,
    hasCover: settings.hasCover,
    autoPageTurnIntervalMs: settings.autoPageTurnIntervalMs,
  };
}

export function sameReaderSettings(
  a: ReaderSettings,
  b: ReaderSettings,
): boolean {
  return (
    a.readingDirection === b.readingDirection &&
    a.pageTurnMode === b.pageTurnMode &&
    a.hasCover === b.hasCover &&
    a.autoPageTurnIntervalMs === b.autoPageTurnIntervalMs
  );
}

export function mergeReaderSettings(
  current: ReaderSettings,
  patch: Partial<ReaderSettings>,
): ReaderSettings {
  const next = { ...current, ...patch };
  return sameReaderSettings(current, next) ? current : next;
}
