import { describe, expect, it } from "vitest";
import {
  calculateProgress,
  clampPageIndex,
  galleryPagesToManga,
  getReaderImageURL,
} from "./reader";

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
  it("builds a cached-image url with the page url encoded", () => {
    const url = getReaderImageURL("https://exhentai.org/s/abc/1");
    expect(url).toContain("/api/cached-image?url=");
    expect(url).toContain(encodeURIComponent("https://exhentai.org/s/abc/1"));
  });
});

describe("galleryPagesToManga", () => {
  it("converts backend pages to comimi image pages", () => {
    const manga = galleryPagesToManga(
      "123",
      "token",
      "Title",
      [
        { page_url: "https://e.org/s/1", index: 0 },
        { page_url: "https://e.org/s/2", index: 1 },
      ],
    );
    expect(manga.id).toBe("123:token");
    expect(manga.title).toBe("Title");
    expect(manga.pages).toHaveLength(2);
    expect(manga.pages[0]).toMatchObject({
      id: "0",
      type: "image",
    });
    const first = manga.pages[0];
    if (first.type === "image") {
      expect(first.src).toContain("/api/cached-image?url=");
    } else {
      throw new Error("expected image page");
    }
  });
});
