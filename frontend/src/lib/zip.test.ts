import { unzipSync } from "fflate";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError, apiBlob } from "../api/client";
import {
  extFromContentType,
  fetchGalleryPages,
  packZip,
  sanitizeFilename,
} from "./zip";

vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>();
  return { ...actual, apiBlob: vi.fn() };
});

const mockedApiBlob = vi.mocked(apiBlob);

function imageBlob(type: string, content = "img"): Blob {
  return new Blob([content], { type });
}

function pageUrls(count: number): string[] {
  return Array.from(
    { length: count },
    (_, i) => `https://exhentai.org/s/abc/${i + 1}`,
  );
}

beforeEach(() => {
  mockedApiBlob.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("sanitizeFilename", () => {
  it("strips filesystem-illegal characters and collapses spaces", () => {
    expect(sanitizeFilename('a/b:c*d?e"f<g>h|i\\j')).toBe(
      "a b c d e f g h i j",
    );
  });

  it("removes control characters", () => {
    expect(sanitizeFilename("a\u0000b\u001fc")).toBe("a b c");
  });

  it("falls back when nothing is left", () => {
    expect(sanitizeFilename("///")).toBe("gallery");
    expect(sanitizeFilename("///", "fallback")).toBe("fallback");
  });

  it("caps the length at 100 characters", () => {
    expect(sanitizeFilename("x".repeat(300)).length).toBe(100);
  });
});

describe("extFromContentType", () => {
  it("maps known content types", () => {
    expect(extFromContentType("image/jpeg", "https://e.org/1")).toBe(".jpg");
    expect(extFromContentType("image/jpg", "https://e.org/1")).toBe(".jpg");
    expect(extFromContentType("image/png", "https://e.org/1")).toBe(".png");
    expect(extFromContentType("image/webp", "https://e.org/1")).toBe(".webp");
    expect(extFromContentType("image/gif", "https://e.org/1")).toBe(".gif");
  });

  it("ignores content type parameters", () => {
    expect(extFromContentType("image/png; charset=binary", "https://e.org/1")).toBe(
      ".png",
    );
  });

  it("falls back to the url extension", () => {
    expect(extFromContentType(null, "https://e.org/a/b.webp")).toBe(".webp");
    expect(extFromContentType(null, "https://e.org/a/b.jpeg")).toBe(".jpg");
  });

  it("defaults to .jpg", () => {
    expect(extFromContentType(null, "https://e.org/a/b")).toBe(".jpg");
    expect(extFromContentType(null, "not a url")).toBe(".jpg");
  });
});

describe("fetchGalleryPages", () => {
  it("downloads pages in order and reports progress", async () => {
    mockedApiBlob
      .mockResolvedValueOnce(imageBlob("image/jpeg", "one"))
      .mockResolvedValueOnce(imageBlob("image/png", "two"));
    const progress: Array<[number, number]> = [];
    const signal = new AbortController().signal;
    const urls = pageUrls(2);

    const files = await fetchGalleryPages({
      pageUrls: urls,
      signal,
      onProgress: (done, total) => progress.push([done, total]),
    });

    expect(Object.keys(files)).toEqual(["001.jpg", "002.png"]);
    expect(progress).toEqual([
      [1, 2],
      [2, 2],
    ]);
    expect(mockedApiBlob).toHaveBeenCalledTimes(2);
    expect(mockedApiBlob).toHaveBeenNthCalledWith(
      1,
      "/api/cached-image",
      { url: urls[0] },
      { signal },
    );
    expect(mockedApiBlob).toHaveBeenNthCalledWith(
      2,
      "/api/cached-image",
      { url: urls[1] },
      { signal },
    );
    const first = files["001.jpg"];
    expect(first).toBeDefined();
    expect(new TextDecoder().decode(first)).toBe("one");
  });

  it("retries server errors and succeeds", async () => {
    vi.useFakeTimers();
    mockedApiBlob
      .mockRejectedValueOnce(new ApiRequestError(502, "upstream failed"))
      .mockResolvedValueOnce(imageBlob("image/jpeg"));

    const promise = fetchGalleryPages({
      pageUrls: pageUrls(1),
      signal: new AbortController().signal,
    });
    await vi.runAllTimersAsync();

    const files = await promise;
    expect(Object.keys(files)).toEqual(["001.jpg"]);
    expect(mockedApiBlob).toHaveBeenCalledTimes(2);
  });

  it("does not retry client errors", async () => {
    mockedApiBlob.mockRejectedValue(
      new ApiRequestError(400, "invalid page url"),
    );

    await expect(
      fetchGalleryPages({
        pageUrls: pageUrls(3),
        signal: new AbortController().signal,
      }),
    ).rejects.toThrow("invalid page url");
    expect(mockedApiBlob).toHaveBeenCalledTimes(1);
  });

  it("gives up after three attempts on server errors", async () => {
    vi.useFakeTimers();
    mockedApiBlob.mockRejectedValue(new ApiRequestError(502, "boom"));

    const promise = fetchGalleryPages({
      pageUrls: pageUrls(1),
      signal: new AbortController().signal,
    });
    const assertion = expect(promise).rejects.toThrow("boom");
    await vi.runAllTimersAsync();
    await assertion;
    expect(mockedApiBlob).toHaveBeenCalledTimes(3);
  });

  it("throws immediately when already aborted", async () => {
    const controller = new AbortController();
    controller.abort();

    await expect(
      fetchGalleryPages({
        pageUrls: pageUrls(1),
        signal: controller.signal,
      }),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(mockedApiBlob).not.toHaveBeenCalled();
  });
});

describe("packZip", () => {
  it("produces a zip blob that unpacks to the same entries", async () => {
    const files = {
      "001.jpg": new Uint8Array([1, 2, 3]),
      "002.png": new Uint8Array([9, 8, 7, 6]),
    };
    const blob = packZip(files);

    expect(blob.type).toBe("application/zip");
    const bytes = new Uint8Array(await blob.arrayBuffer());
    expect(bytes[0]).toBe(0x50);
    expect(bytes[1]).toBe(0x4b);

    const unpacked = unzipSync(bytes);
    expect(Object.keys(unpacked).sort()).toEqual(["001.jpg", "002.png"]);
    expect(Array.from(unpacked["001.jpg"])).toEqual([1, 2, 3]);
    expect(Array.from(unpacked["002.png"])).toEqual([9, 8, 7, 6]);
  });
});
