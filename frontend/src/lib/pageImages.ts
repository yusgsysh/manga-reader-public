/**
 * Comic page images for the book reader.
 *
 * Every page the reader shows — the DOM page and, during a turn, the WebGL
 * texture — comes from this one cache: a page URL is resolved once through the
 * reader's existing image endpoint, decoded once, and the resulting
 * `HTMLImageElement` is handed to both. That keeps a turn from requesting or
 * decoding the same image twice, and lets the texture arrive before the page
 * starts folding.
 *
 * The cache is deliberately small: a window of pages around the reading
 * position is pinned, the rest is dropped least-recently-used first, and the
 * byte budget keeps a long gallery from growing GPU and memory use page by
 * page. Loads are queued with a concurrency limit so preloading ahead cannot
 * starve the visible page.
 */

export type PageUrlResolver = (pageIndex: number) => Promise<string>;
export type PageImageLoader = (url: string) => Promise<HTMLImageElement>;

export interface PageImageCacheOptions {
  /** Maps a 0-based comic page index to the reader's image URL. */
  resolve: PageUrlResolver;
  /** Decoded-image budget in bytes. Pages around the view are pinned first. */
  maxBytes?: number;
  /** Hard cap on cached page images. */
  maxImages?: number;
  /** How many images may decode at once. */
  concurrency?: number;
  /** Override the image loader (tests, or a non-DOM decoder). */
  loadImage?: PageImageLoader;
}

type EntryStatus = "loading" | "ready" | "error";

interface Entry {
  index: number;
  status: EntryStatus;
  url?: string;
  image?: HTMLImageElement;
  bytes: number;
  /** Monotonic use counter for least-recently-used eviction. */
  used: number;
  promise?: Promise<HTMLImageElement>;
}

interface Task {
  index: number;
  resolve: (image: HTMLImageElement) => void;
  reject: (error: unknown) => void;
}

const DEFAULT_MAX_BYTES = 96 * 1024 * 1024;
const DEFAULT_MAX_IMAGES = 10;
const DEFAULT_CONCURRENCY = 3;

/** The image's decoded size, the way the GPU would hold it (RGBA). */
function decodedBytes(image: HTMLImageElement): number {
  const width = image.naturalWidth || 0;
  const height = image.naturalHeight || 0;
  return width * height * 4;
}

function loadWithElement(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.decoding = "async";
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error(`page image failed: ${url}`));
    image.src = url;
  });
}

export class PageImageCache {
  private readonly resolve: PageUrlResolver;
  private readonly loadImage: PageImageLoader;
  private readonly maxBytes: number;
  private readonly maxImages: number;
  private readonly concurrency: number;

  private readonly entries = new Map<number, Entry>();
  private readonly tasks: Task[] = [];
  private readonly pinned = new Set<number>();
  private active = 0;
  private bytes = 0;
  private clock = 0;
  private destroyed = false;

  constructor(options: PageImageCacheOptions) {
    this.resolve = options.resolve;
    this.loadImage = options.loadImage ?? loadWithElement;
    this.maxBytes = Math.max(1, options.maxBytes ?? DEFAULT_MAX_BYTES);
    this.maxImages = Math.max(1, options.maxImages ?? DEFAULT_MAX_IMAGES);
    this.concurrency = Math.max(1, options.concurrency ?? DEFAULT_CONCURRENCY);
  }

  /** The decoded image, loading it if needed. */
  load(pageIndex: number): Promise<HTMLImageElement> {
    const existing = this.entries.get(pageIndex);
    if (existing) {
      existing.used = ++this.clock;
      if (existing.status === "ready" && existing.image) {
        return Promise.resolve(existing.image);
      }
      if (existing.status === "loading" && existing.promise) {
        return existing.promise;
      }
      return Promise.reject(new Error(`page image failed: ${pageIndex}`));
    }
    if (this.destroyed) {
      return Promise.reject(new Error("page image cache disposed"));
    }

    const entry: Entry = {
      index: pageIndex,
      status: "loading",
      bytes: 0,
      used: ++this.clock,
    };
    this.entries.set(pageIndex, entry);
    entry.promise = new Promise<HTMLImageElement>((resolve, reject) => {
      this.tasks.push({ index: pageIndex, resolve, reject });
      this.pump();
    });
    // Preloads are fire-and-forget; mark the rejection as observed so a page
    // that fails while out of sight cannot surface as an unhandled rejection.
    entry.promise.catch(() => undefined);
    return entry.promise;
  }

  /** The loaded image, if it is already in the cache. */
  peek(pageIndex: number): HTMLImageElement | null {
    const entry = this.entries.get(pageIndex);
    if (!entry || entry.status !== "ready" || !entry.image) return null;
    entry.used = ++this.clock;
    return entry.image;
  }

  /** The resolved image URL, once known — what a DOM page should show. */
  url(pageIndex: number): string | undefined {
    return this.entries.get(pageIndex)?.url;
  }

  /**
   * Pins the pages around the reading position and starts decoding them.
   * Everything outside is fair game for eviction.
   */
  retain(pageIndexes: readonly number[]): void {
    this.pinned.clear();
    for (const index of pageIndexes) {
      this.pinned.add(index);
      this.load(index).catch(() => undefined);
    }
    this.evict();
  }

  /** Forgets a failed page so the next load tries again. */
  retry(pageIndex: number): Promise<HTMLImageElement> {
    const entry = this.entries.get(pageIndex);
    if (entry) {
      this.bytes -= entry.bytes;
      this.entries.delete(pageIndex);
      entry.status = "error";
      entry.image = undefined;
    }
    return this.load(pageIndex);
  }

  /** Drops every decoded image. The cache stays usable afterwards. */
  clear(): void {
    this.entries.clear();
    this.pinned.clear();
    this.bytes = 0;
  }

  /** Clears the cache and refuses further loads (component unmount). */
  dispose(): void {
    this.destroyed = true;
    for (const task of this.tasks.splice(0)) {
      task.reject(new Error("page image cache disposed"));
    }
    this.clear();
  }

  /** Images currently held. Exposed for tests and the reader's diagnostics. */
  get size(): number {
    return this.entries.size;
  }

  /** Decoded bytes currently held. */
  get byteSize(): number {
    return this.bytes;
  }

  private pump(): void {
    if (this.destroyed) return;
    while (this.active < this.concurrency && this.tasks.length > 0) {
      const task = this.tasks.shift();
      if (!task) break;
      const entry = this.entries.get(task.index);
      if (!entry || entry.status !== "loading") {
        task.reject(new Error(`page image cancelled: ${task.index}`));
        continue;
      }
      this.active += 1;
      void this.run(task, entry);
    }
  }

  private async run(task: Task, entry: Entry): Promise<void> {
    try {
      const url = await this.resolve(entry.index);
      if (this.destroyed) throw new Error("page image cache disposed");
      const image = await this.loadImage(url);
      // The cache may have been cleared, evicted or disposed while the image
      // was decoding; a stale result must not re-enter the accounting.
      if (this.destroyed || this.entries.get(entry.index) !== entry) {
        throw new Error("page image cancelled");
      }
      entry.url = url;
      entry.image = image;
      entry.status = "ready";
      entry.bytes = decodedBytes(image);
      entry.used = ++this.clock;
      this.bytes += entry.bytes;
      task.resolve(image);
    } catch (error) {
      entry.status = "error";
      entry.image = undefined;
      task.reject(error);
    } finally {
      this.active = Math.max(0, this.active - 1);
      this.evict();
      this.pump();
    }
  }

  private evict(): void {
    if (this.entries.size <= this.maxImages && this.bytes <= this.maxBytes) {
      return;
    }
    const candidates = [...this.entries.values()]
      // A load in flight has no bytes to reclaim yet and is about to be shown.
      .filter((entry) => entry.status !== "loading" && !this.pinned.has(entry.index))
      .sort((a, b) => a.used - b.used);
    for (const entry of candidates) {
      if (this.entries.size <= this.maxImages && this.bytes <= this.maxBytes) {
        break;
      }
      this.entries.delete(entry.index);
      this.bytes -= entry.bytes;
    }
  }
}
