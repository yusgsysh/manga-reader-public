import type { ImagePage, ReactManga } from "@yui540/comimi-react";
import { pageImageUrl, pageThumbnailUrl } from "./image";

export function getReaderImageURL(pageURL: string): string {
  return pageImageUrl(pageURL);
}

// Page slots share one constant `src` so comimi-react's manga signature
// (`JSON.stringify([id, pages.map(...)])`) never changes as real page URLs
// stream in. The reader image is resolved lazily via `resolvePageSrc`, and the
// page list uses `thumbnailSrc`, so neither reads this field.
const SLOT_SRC = "";

// A page whose URL never arrived (upstream page count below the authoritative
// gallery total) resolves to this so comimi shows its error mascot instead of
// spinning forever. The truncated base64 is not a decodable image, so the
// browser fires `error` on it without any network request.
const MISSING_PAGE_SRC = "data:image/gif;base64,R0lGOD";

interface Waiter {
  resolve: (url: string) => void;
}

/**
 * Collects page URLs as they stream in and hands them to comimi on demand.
 * `wait` resolves once the URL for an index arrives (or with a broken-image
 * sentinel if the stream ends short); this lets pages be pre-allocated from the
 * gallery total while their `src` is filled lazily.
 */
export class PageUrlStore {
  private urls = new Map<number, string>();
  private waiters = new Map<number, Waiter[]>();
  private failed = false;

  set(index: number, url: string): void {
    if (this.urls.get(index) === url) return;
    this.urls.set(index, url);
    const waiters = this.waiters.get(index);
    if (waiters) {
      this.waiters.delete(index);
      for (const waiter of waiters) waiter.resolve(url);
    }
  }

  get(index: number): string | undefined {
    return this.urls.get(index);
  }

  wait(index: number): Promise<string> {
    const url = this.urls.get(index);
    if (url !== undefined) return Promise.resolve(url);
    // Never reject: comimi-react does not handle a rejection from
    // `resolvePageSrc`, so a broken sentinel is the safe terminal value.
    if (this.failed) return Promise.resolve(MISSING_PAGE_SRC);
    return new Promise((resolve) => {
      const waiters = this.waiters.get(index) ?? [];
      waiters.push({ resolve });
      this.waiters.set(index, waiters);
    });
  }

  /** Resolves every pending waiter with the broken sentinel. */
  failAll(): void {
    if (this.failed) return;
    this.failed = true;
    const waiters = [...this.waiters.values()].flat();
    this.waiters.clear();
    for (const waiter of waiters) waiter.resolve(MISSING_PAGE_SRC);
  }

  reset(): void {
    this.urls.clear();
    this.waiters.clear();
    this.failed = false;
  }
}

/**
 * Builds a manga with exactly `total` stable page slots, so comimi's page-list
 * key (`locale:manga.id:pages.length`) and manga signature stay constant as the
 * live page list streams in. Every slot is addressed by index, both for its
 * thumbnail and (through `resolvePageSrc`) its full image.
 */
export function galleryPageSlotsToManga(
  id: string,
  token: string,
  title: string,
  total: number,
): ReactManga {
  return {
    id: `${id}:${token}`,
    title,
    pages: Array.from({ length: Math.max(0, total) }, (_, index) => ({
      id: `${index}`,
      type: "image" as const,
      src: SLOT_SRC,
      thumbnailSrc: token ? pageThumbnailUrl(id, token, index) : undefined,
      alt: `${title} - ${index + 1}`,
    })),
  };
}

export type SlotPageSrcResolver = (context: {
  page: ImagePage;
  pageIndex: number;
  isSpread: boolean;
}) => Promise<string>;

/** Resolves a slot's real page image URL from the streaming URL store. */
export function slotPageSrcResolver(
  store: PageUrlStore,
): SlotPageSrcResolver {
  return async ({ pageIndex }) => {
    const url = await store.wait(pageIndex);
    return url === MISSING_PAGE_SRC ? url : getReaderImageURL(url);
  };
}

/**
 * Parses a `?page=` query value (0-indexed). Returns null for missing or
 * invalid input.
 */
export function parsePageParam(raw: string | null): number | null {
  if (raw === null) return null;
  if (!/^\d+$/.test(raw)) return null;
  const value = Number(raw);
  return Number.isSafeInteger(value) ? value : null;
}

export function clampPageIndex(pageIndex: number, total: number): number {
  if (total <= 0) return 0;
  return Math.min(Math.max(Math.round(pageIndex), 0), total - 1);
}

export function calculateProgress(
  currentPage: number,
  total: number,
): { progress: number; completed: boolean } {
  if (total <= 0) return { progress: 0, completed: false };
  const isLastPage = currentPage >= total - 1;
  if (isLastPage) return { progress: 1, completed: true };
  return {
    progress: Math.min(Math.max((currentPage + 1) / total, 0), 1),
    completed: false,
  };
}
