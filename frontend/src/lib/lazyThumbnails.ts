// comimi renders every page-list / seek-preview thumbnail eagerly, so a large
// gallery fires hundreds of `/image-cache/page-thumbnail` requests at once. This
// module defers those images and feeds them through a small concurrency-limited
// queue, loading only what is (nearly) on screen.

const PLACEHOLDER =
  "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";

const LAZY_FLAG = "lazyThumb";
// The original URL is kept on the node so a later run (effect re-run,
// re-parented node) can adopt images that a previous run already deferred.
const LAZY_SRC = "lazyThumbSrc";

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

export interface QueueItem {
  img: HTMLImageElement;
  url: string;
}

export interface ThumbnailQueue {
  enqueue: (item: QueueItem) => void;
  stop: () => void;
}

/**
 * Runs image loads with at most `concurrency` in flight. A slot is freed when
 * the image fires `load` or `error`.
 */
export function createThumbnailQueue(concurrency = 6): ThumbnailQueue {
  let active = 0;
  let stopped = false;
  const waiting: QueueItem[] = [];

  const pump = () => {
    while (!stopped && active < concurrency && waiting.length > 0) {
      const item = waiting.shift()!;
      active++;

      const release = () => {
        item.img.removeEventListener("load", release);
        item.img.removeEventListener("error", release);
        active--;
        pump();
      };
      item.img.addEventListener("load", release);
      item.img.addEventListener("error", release);

      item.img.src = item.url;
    }
  };

  return {
    enqueue(item) {
      if (stopped) return;
      waiting.push(item);
      pump();
    },
    stop() {
      stopped = true;
      waiting.length = 0;
    },
  };
}

export interface LazyThumbnailOptions {
  concurrency?: number;
  rootMargin?: string;
}

/**
 * Observes `root` for comimi thumbnails and loads them lazily through a
 * concurrency-limited queue. Returns a cleanup function.
 */
export function observeLazyThumbnails(
  root: HTMLElement,
  { concurrency = 6, rootMargin = "300px" }: LazyThumbnailOptions = {},
): () => void {
  const queue = createThumbnailQueue(concurrency);
  const pending = new WeakMap<HTMLImageElement, string>();

  // The IO root must be the viewport, never `root` itself: in fullscreen
  // comimi makes `.comimi-root` a viewport-fixed box, and Chromium reports a
  // 0x0 rootBounds for every *ancestor* of such an element (including the
  // fullscreen element), so nothing would ever intersect and the placeholders
  // would stick forever. The viewport root keeps the ancestor clipping that
  // actually gates loading (the height-0 menu, the page-list scroller).
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const img = entry.target as HTMLImageElement;
        observer.unobserve(img);
        const url = pending.get(img);
        if (url) queue.enqueue({ img, url });
      }
    },
    { root: null, rootMargin },
  );

  const defer = (node: Node) => {
    if (!(node instanceof HTMLImageElement)) return;
    const alreadyDeferred = node.dataset[LAZY_FLAG] === "1";
    const url = alreadyDeferred ? node.dataset[LAZY_SRC] : node.src;
    if (!url || !isPageThumbnailURL(url)) return;
    if (alreadyDeferred) {
      // Re-adopt an image deferred by an earlier run: its observer is gone,
      // so without this it would sit on the placeholder for good. Skip images
      // that already carry a real src — those are loading or loaded.
      if (node.getAttribute("src") !== PLACEHOLDER) return;
    } else {
      node.dataset[LAZY_FLAG] = "1";
      node.dataset[LAZY_SRC] = url;
      node.src = PLACEHOLDER;
    }
    pending.set(node, url);
    observer.observe(node);
  };

  const scan = (node: Node) => {
    defer(node);
    if (node instanceof Element) {
      node.querySelectorAll("img").forEach(defer);
    }
  };

  const unbind = (node: Node) => {
    if (node instanceof HTMLImageElement) {
      observer.unobserve(node);
    } else if (node instanceof Element) {
      node.querySelectorAll("img").forEach((img) => observer.unobserve(img));
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
    observer.disconnect();
    queue.stop();
  };
}
