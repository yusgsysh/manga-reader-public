/**
 * The reader settings both backends share.
 *
 * comimi owns its full settings store, but the reader also has a grabfold book
 * view that comimi does not own. Keeping this subset in one place lets the app
 * chrome drive whichever viewer is mounted without either one holding a second
 * copy of the reading position or the direction.
 */

import type { ViewerSettings } from "@yui540/comimi-react";

export interface ReaderSettings {
  readingDirection: "rtl" | "ltr";
  pageTurnMode: "single" | "spread";
  pageTurnAnimation: "slide" | "book";
  hasCover: boolean;
  autoPageTurnIntervalMs: number;
}

export const DEFAULT_READER_SETTINGS: ReaderSettings = Object.freeze({
  readingDirection: "rtl",
  pageTurnMode: "single",
  pageTurnAnimation: "slide",
  hasCover: true,
  autoPageTurnIntervalMs: 5000,
});

export function pickReaderSettings(settings: ViewerSettings): ReaderSettings {
  return {
    readingDirection: settings.readingDirection,
    pageTurnMode: settings.pageTurnMode,
    pageTurnAnimation: settings.pageTurnAnimation,
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
    a.pageTurnAnimation === b.pageTurnAnimation &&
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
