import { describe, expect, it } from "vitest";
import { PageImageCache, type PageImageCacheOptions } from "./pageImages";

/** The parts of an image the cache reads, without a DOM. */
function fakeImage(url: string, width = 1000, height = 1500) {
  return {
    src: url,
    naturalWidth: width,
    naturalHeight: height,
  } as unknown as HTMLImageElement;
}

function cache(options: Partial<PageImageCacheOptions> = {}) {
  const requested: number[] = [];
  const instance = new PageImageCache({
    resolve: async (index) => {
      requested.push(index);
      return `page-${index}.jpg`;
    },
    loadImage: async (url) => fakeImage(url),
    ...options,
  });
  return { instance, requested };
}

describe("PageImageCache", () => {
  it("resolves and decodes each page once", async () => {
    const { instance, requested } = cache();
    const [first, second] = await Promise.all([
      instance.load(3),
      instance.load(3),
    ]);
    expect(first).toBe(second);
    expect(requested).toEqual([3]);
    expect(instance.url(3)).toBe("page-3.jpg");
    expect(instance.size).toBe(1);
    await instance.load(3);
    expect(requested).toEqual([3]);
  });

  it("pins and preloads the pages around the view", async () => {
    const { instance, requested } = cache();
    instance.retain([1, 2, 3]);
    await Promise.resolve();
    await Promise.resolve();
    expect(requested.sort()).toEqual([1, 2, 3]);
  });

  it("never runs more than the allowed number of decodes at once", async () => {
    let active = 0;
    let peak = 0;
    const { instance } = cache({
      concurrency: 2,
      loadImage: async (url) => {
        active += 1;
        peak = Math.max(peak, active);
        await Promise.resolve();
        await Promise.resolve();
        active -= 1;
        return fakeImage(url);
      },
    });
    await Promise.all([0, 1, 2, 3, 4].map((index) => instance.load(index)));
    expect(peak).toBe(2);
  });

  it("keeps the cache bounded and drops the least recently used page", async () => {
    const { instance } = cache({ maxImages: 2, maxBytes: 1 << 30 });
    await instance.load(0);
    await instance.load(1);
    await instance.load(2);
    expect(instance.size).toBe(2);
    expect(instance.peek(0)).toBeNull();
    expect(instance.peek(1)).not.toBeNull();
    expect(instance.peek(2)).not.toBeNull();
  });

  it("keeps the byte budget, not just a page count", async () => {
    const pageBytes = 1000 * 1500 * 4;
    const { instance } = cache({
      maxImages: 10,
      maxBytes: pageBytes * 2 + 1,
    });
    await instance.load(0);
    await instance.load(1);
    await instance.load(2);
    expect(instance.byteSize).toBeLessThanOrEqual(pageBytes * 2 + 1);
    expect(instance.size).toBe(2);
  });

  it("can retry a page that failed to load", async () => {
    let attempts = 0;
    const { instance } = cache({
      loadImage: async (url) => {
        attempts += 1;
        if (attempts === 1) throw new Error("network");
        return fakeImage(url);
      },
    });

    await expect(instance.load(4)).rejects.toThrow("network");
    await expect(instance.load(4)).rejects.toThrow("page image failed");
    const image = await instance.retry(4);
    expect(image.src).toBe("page-4.jpg");
    expect(attempts).toBe(2);
  });

  it("drops a decode that finishes after the cache was cleared", async () => {
    let release: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const { instance } = cache({
      loadImage: async (url) => {
        await gate;
        return fakeImage(url);
      },
    });
    const loading = instance.load(0);
    instance.clear();
    release();
    await expect(loading).rejects.toThrow("cancelled");
    expect(instance.size).toBe(0);
    expect(instance.byteSize).toBe(0);
  });

  it("clears everything on dispose and refuses later loads", async () => {
    const { instance } = cache();
    await instance.load(0);
    instance.dispose();
    expect(instance.size).toBe(0);
    await expect(instance.load(1)).rejects.toThrow("disposed");
  });
});
