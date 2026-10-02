import { describe, expect, it } from "vitest";
import {
  calculateProgress,
  clampPageIndex,
  galleryPageSlotsToManga,
  getReaderImageURL,
  PageUrlStore,
  parsePageParam,
  slotPageSrcResolver,
} from "./reader";

describe("parsePageParam", () => {
  it("parses non-negative integers", () => {
    expect(parsePageParam("0")).toBe(0);
    expect(parsePageParam("42")).toBe(42);
  });

  it("rejects missing or invalid values", () => {
    expect(parsePageParam(null)).toBeNull();
    expect(parsePageParam("")).toBeNull();
    expect(parsePageParam("-1")).toBeNull();
    expect(parsePageParam("1.5")).toBeNull();
    expect(parsePageParam("abc")).toBeNull();
  });
});

describe("clampPageIndex", () => {
  it("keeps in-range values", () => {
    expect(clampPageIndex(5, 24)).toBe(5);
    expect(clampPageIndex(0, 24)).toBe(0);
    expect(clampPageIndex(23, 24)).toBe(23);
  });

  it("clamps below 0 and above last page", () => {
    expect(clampPageIndex(-1, 24)).toBe(0);
    expect(clampPageIndex(99, 24)).toBe(23);
  });

  it("handles empty galleries", () => {
    expect(clampPageIndex(0, 0)).toBe(0);
  });
});

describe("calculateProgress", () => {
  it("uses 0-based index with 1-based progress", () => {
    expect(calculateProgress(0, 24)).toEqual({
      progress: 1 / 24,
      completed: false,
    });
    expect(calculateProgress(10, 24)).toEqual({
      progress: 11 / 24,
      completed: false,
    });
  });

  it("marks the last page completed with progress 1", () => {
    expect(calculateProgress(23, 24)).toEqual({
      progress: 1,
      completed: true,
    });
  });

  it("handles empty galleries", () => {
    expect(calculateProgress(0, 0)).toEqual({
      progress: 0,
      completed: false,
    });
  });
});

describe("getReaderImageURL", () => {
  it("builds the cached page image url with the page url encoded", () => {
    const url = getReaderImageURL("https://exhentai.org/s/abc/1");
    expect(url).toContain("/api/image-cache/page?url=");
    expect(url).toContain(encodeURIComponent("https://exhentai.org/s/abc/1"));
  });
});

describe("galleryPageSlotsToManga", () => {
  it("pre-allocates exactly `total` stable, index-addressed slots", () => {
    const manga = galleryPageSlotsToManga("123", "token", "Title", 3);
    expect(manga.id).toBe("123:token");
    expect(manga.title).toBe("Title");
    expect(manga.pages).toHaveLength(3);

    manga.pages.forEach((page, index) => {
      if (page.type !== "image") throw new Error("expected image page");
      expect(page.id).toBe(String(index));
      expect(page.alt).toBe(`Title - ${index + 1}`);
      // The signature-relevant fields never depend on the streamed URL, so
      // comimi-react never calls setManga once the slots are built.
      expect(page.src).toBe("");
      expect(page.thumbnailSrc).toContain("/api/image-cache/page-thumbnail?");
      expect(page.thumbnailSrc).toContain("id=123");
      expect(page.thumbnailSrc).toContain("token=token");
      expect(page.thumbnailSrc).toContain(`index=${index}`);
      expect(page.thumbnailSrc).not.toContain("url=");
    });
  });

  it("skips thumbnails when the gallery token is missing", () => {
    const manga = galleryPageSlotsToManga("123", "", "Title", 1);
    const page = manga.pages[0];
    if (page.type !== "image") throw new Error("expected image page");
    expect(page.thumbnailSrc).toBeUndefined();
  });

  it("handles a zero total", () => {
    expect(galleryPageSlotsToManga("123", "token", "Title", 0).pages).toEqual(
      [],
    );
  });
});

describe("PageUrlStore", () => {
  it("resolves waiters when the page url arrives", async () => {
    const store = new PageUrlStore();
    const pending = store.wait(2);
    store.set(2, "https://e.org/s/3");
    await expect(pending).resolves.toBe("https://e.org/s/3");
    expect(store.get(2)).toBe("https://e.org/s/3");
  });

  it("resolves immediately when the url is already known", async () => {
    const store = new PageUrlStore();
    store.set(0, "https://e.org/s/1");
    await expect(store.wait(0)).resolves.toBe("https://e.org/s/1");
  });

  it("resolves missing pages to a broken sentinel on failAll", async () => {
    const store = new PageUrlStore();
    const pending = store.wait(5);
    store.failAll();
    await expect(pending).resolves.toMatch(/^data:/);
    // Later waits must not hang.
    await expect(store.wait(6)).resolves.toMatch(/^data:/);
  });

  it("reset clears known urls", async () => {
    const store = new PageUrlStore();
    store.set(0, "https://e.org/s/1");
    store.reset();
    expect(store.get(0)).toBeUndefined();
  });
});

describe("slotPageSrcResolver", () => {
  it("maps a slot index to the cached page image url", async () => {
    const store = new PageUrlStore();
    store.set(1, "https://e.org/s/2");
    const resolve = slotPageSrcResolver(store);
    const src = await resolve({
      page: { id: "1", type: "image", src: "" },
      pageIndex: 1,
      isSpread: false,
    });
    expect(src).toContain("/api/image-cache/page?url=");
    expect(src).toContain(encodeURIComponent("https://e.org/s/2"));
  });
});
