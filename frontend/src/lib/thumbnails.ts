// Page lists (the app's reader/gallery grids) render every thumbnail eagerly,
// so a large gallery fires hundreds of `/image-cache/page-thumbnail` requests at
// once. This module intercepts those images and feeds all of them through a
// small concurrency-limited queue: every thumbnail is loaded as soon as it
// appears, never gated on the viewport, but only `concurrency` requests are in
// flight.

const LAZY_FLAG = "lazyThumb";
// The original URL is kept on the node so a later run (effect re-run,
// re-parented node) can adopt images that a previous run already deferred.
const LAZY_SRC = "lazyThumbSrc";

// Shared cap for /page-thumbnail requests, used by the reader and gallery
// detail page lists.
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

interface QueueItem {
  img: HTMLImageElement;
  url: string;
}

interface ThumbnailQueue {
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

interface LoadThumbnailsOptions {
  concurrency?: number;
}

/** Cleanup for {@link loadThumbnails}. */
export type LoadThumbnailsHandle = () => void;

/**
 * Watches `root` for page thumbnails and loads every one of them through a
 * concurrency-limited queue as soon as it appears. Returns a cleanup function.
 */
export function loadThumbnails(
  root: HTMLElement,
  { concurrency = THUMBNAIL_CONCURRENCY }: LoadThumbnailsOptions = {},
): LoadThumbnailsHandle {
  const queue = createThumbnailQueue(concurrency);

  const defer = (node: Node) => {
    if (!(node instanceof HTMLImageElement)) return;
    // comimi builds the seek-preview thumbnail on demand (hover over the seek
    // bar) and sets its src directly. Intercepting it would remove that src
    // until a queue slot frees up, leaving the hover preview blank. Leave it to
    // load natively.
    if (node.closest(".comimi-seek-preview")) return;
    const stored = node.dataset[LAZY_SRC];
    const url = stored ?? node.src;
    if (!url || !isPageThumbnailURL(url)) return;
    if (node.dataset[LAZY_FLAG] !== "1") {
      node.dataset[LAZY_FLAG] = "1";
      node.dataset[LAZY_SRC] = url;
      // Clear src instead of pointing it at a placeholder image: a 1x1 GIF
      // placeholder fires its own `load`, which consumers (React onLoad,
      // comimi) mistake for the real thumbnail and hide the loading shimmer
      // for every still-queued tile. A src-less <img> neither loads nor fires
      // events, so the shimmer stays until the queue assigns the real URL.
      node.removeAttribute("src");
    } else if (node.hasAttribute("src")) {
      // Already loading or loaded (possibly by a previous run).
      return;
    }
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

  return () => {
    mutations.disconnect();
    queue.stop();
  };
}
