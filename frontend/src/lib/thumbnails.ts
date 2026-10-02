// comimi renders every page-list / seek-preview thumbnail eagerly, so a large
// gallery fires hundreds of `/image-cache/page-thumbnail` requests at once. This
// module intercepts those images and feeds all of them through a small
// concurrency-limited queue: every thumbnail is loaded as soon as it appears,
// never gated on the viewport, but only `concurrency` requests are in flight.

const PLACEHOLDER =
  "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";

const LAZY_FLAG = "lazyThumb";
// The original URL is kept on the node so a later run (effect re-run,
// re-parented node) can adopt images that a previous run already deferred.
const LAZY_SRC = "lazyThumbSrc";

// Shared cap for /page-thumbnail requests, used by both the reader's eager
// page-list thumbnails and the gallery detail's paginated grid.
export const THUMBNAIL_CONCURRENCY = 5;

export function isPageThumbnailURL(url: string): boolean {
  try {
    const base =
      typeof window !== "undefined" ? window.location.origin : undefined;
    const parsed = new URL(url, base);
    return (
      parsed.pathname.endsWith("/page-thumbnail") &&
      (parsed.searchParams.has("url") || parsed.searchParams.has("index"))
    );
  } catch {
    return false;
  }
}

/** Returns the gallery page index of an index-addressed thumbnail URL. */
export function pageThumbnailIndex(url: string): number | null {
  try {
    const base =
      typeof window !== "undefined" ? window.location.origin : undefined;
    const parsed = new URL(url, base);
    if (!parsed.pathname.endsWith("/page-thumbnail")) return null;
    const raw = parsed.searchParams.get("index");
    if (raw === null) return null;
    const value = Number(raw);
    return Number.isInteger(value) && value >= 0 ? value : null;
  } catch {
    return null;
  }
}

export interface QueueItem {
  img: HTMLImageElement;
  url: string;
}

export interface ThumbnailQueue {
  enqueue: (item: QueueItem) => void;
  cancel: (img: HTMLImageElement) => void;
  stop: () => void;
}

/**
 * Runs image loads with at most `concurrency` in flight. A slot is freed when
 * the image fires `load` or `error`, or when it is cancelled (e.g. removed from
 * the DOM before it finished).
 */
export function createThumbnailQueue(concurrency = 5): ThumbnailQueue {
  let active = 0;
  let stopped = false;
  const waiting: QueueItem[] = [];
  const inFlight = new Map<HTMLImageElement, () => void>();

  const pump = () => {
    while (!stopped && active < concurrency && waiting.length > 0) {
      const item = waiting.shift()!;
      active++;

      const release = () => {
        if (!inFlight.delete(item.img)) return;
        item.img.removeEventListener("load", release);
        item.img.removeEventListener("error", release);
        active--;
        pump();
      };
      inFlight.set(item.img, release);
      item.img.addEventListener("load", release);
      item.img.addEventListener("error", release);

      item.img.src = item.url;
    }
  };

  return {
    enqueue(item) {
      if (stopped) return;
      if (inFlight.has(item.img)) return;
      if (waiting.some((queued) => queued.img === item.img)) return;
      waiting.push(item);
      pump();
    },
    cancel(img) {
      const waitingIndex = waiting.findIndex((item) => item.img === img);
      if (waitingIndex !== -1) {
        waiting.splice(waitingIndex, 1);
        return;
      }
      inFlight.get(img)?.();
    },
    stop() {
      stopped = true;
      waiting.length = 0;
    },
  };
}

export interface LoadThumbnailsOptions {
  concurrency?: number;
  /**
   * When set, only thumbnails with a page index below the returned count are
   * enqueued; the rest keep their placeholder until a later `refresh()`. Used by
   * the reader so prefetching never runs ahead of the streamed page list.
   * Thumbnails without an index (sprite-rectangle form) are always allowed.
   */
  getLimit?: () => number;
}

/** Cleanup for {@link loadThumbnails}; call `refresh` to re-scan for newly
 *  eligible thumbnails after the limit grows. */
export interface LoadThumbnailsHandle {
  (): void;
  refresh: () => void;
}

/**
 * Watches `root` for comimi thumbnails and loads every one of them through a
 * concurrency-limited queue as soon as it appears. Returns a callable cleanup
 * handle with a `refresh` method.
 */
export function loadThumbnails(
  root: HTMLElement,
  {
    concurrency = THUMBNAIL_CONCURRENCY,
    getLimit,
  }: LoadThumbnailsOptions = {},
): LoadThumbnailsHandle {
  const queue = createThumbnailQueue(concurrency);

  const allowed = (url: string): boolean => {
    if (!getLimit) return true;
    const index = pageThumbnailIndex(url);
    if (index === null) return true;
    return index < getLimit();
  };

  const defer = (node: Node) => {
    if (!(node instanceof HTMLImageElement)) return;
    const stored = node.dataset[LAZY_SRC];
    const url = stored ?? node.src;
    if (!url || !isPageThumbnailURL(url)) return;
    if (node.dataset[LAZY_FLAG] !== "1") {
      node.dataset[LAZY_FLAG] = "1";
      node.dataset[LAZY_SRC] = url;
      node.src = PLACEHOLDER;
    } else if (node.getAttribute("src") !== PLACEHOLDER) {
      // Already loading or loaded (possibly by a previous run).
      return;
    }
    // Still a placeholder. Skip if it is beyond the current limit; a later
    // refresh() re-runs defer and enqueues it once the limit has grown.
    if (!allowed(url)) return;
    // Queue it now, or re-queue it if an earlier run was cleaned up while it
    // was waiting.
    queue.enqueue({ img: node, url });
  };

  const scan = (node: Node) => {
    defer(node);
    if (node instanceof Element) {
      node.querySelectorAll("img").forEach(defer);
    }
  };

  const unbind = (node: Node) => {
    if (node instanceof HTMLImageElement) {
      queue.cancel(node);
    } else if (node instanceof Element) {
      node.querySelectorAll("img").forEach((img) => queue.cancel(img));
    }
  };

  const mutations = new MutationObserver((records) => {
    for (const record of records) {
      record.addedNodes.forEach(scan);
      record.removedNodes.forEach(unbind);
    }
  });
  mutations.observe(root, { childList: true, subtree: true });

  scan(root);

  const cleanup = (() => {
    mutations.disconnect();
    queue.stop();
  }) as LoadThumbnailsHandle;
  cleanup.refresh = () => scan(root);
  return cleanup;
}
