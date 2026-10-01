import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchGalleryPages, fetchGalleryPagesWithFallback } from "./gallery";
import type { GalleryPagesResponse } from "../types/reader";

const BASE = "http://localhost:8080";

const pages: GalleryPagesResponse = {
  id: "123",
  token: "tok",
  total: 1,
  pages: [{ page_url: "https://exhentai.org/s/a/1", index: 0 }],
};

const fetchMock = vi.fn();
const originalFetch = globalThis.fetch;

beforeEach(() => {
  fetchMock.mockReset();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function ndjsonResponse(...events: unknown[]): Response {
  const body = events.map((event) => `${JSON.stringify(event)}\n`).join("");
  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "application/x-ndjson" },
  });
}

function liveStream(): Response {
  return ndjsonResponse(
    { type: "meta", id: "123", token: "tok", total: 2 },
    { type: "page", page_url: "https://exhentai.org/s/a/1", index: 0 },
    { type: "page", page_url: "https://exhentai.org/s/a/2", index: 1 },
    { type: "done", total: 2 },
  );
}

describe("fetchGalleryPages", () => {
  it("aggregates the NDJSON stream and reports progressive snapshots", async () => {
    fetchMock.mockResolvedValue(liveStream());
    const snapshots: GalleryPagesResponse[] = [];

    const result = await fetchGalleryPages(123, "tok", (snapshot) =>
      snapshots.push(snapshot),
    );

    expect(result).toEqual({
      id: "123",
      token: "tok",
      total: 2,
      pages: [
        { page_url: "https://exhentai.org/s/a/1", index: 0 },
        { page_url: "https://exhentai.org/s/a/2", index: 1 },
      ],
    });
    expect(snapshots.map((s) => s.pages.length)).toEqual([1, 2]);
    expect(snapshots[1].id).toBe("123");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe(
      `${BASE}/api/gallery/123/tok/pages`,
    );
  });

  it("rejects when the stream reports an error line", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(
        { type: "meta", id: "123", token: "tok", total: 65 },
        { type: "page", page_url: "https://exhentai.org/s/a/1", index: 0 },
        { type: "error", error: "fetch gallery pages failed: upstream broke" },
      ),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "upstream broke",
    );
  });

  it("rejects when the stream ends without a terminal line", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(
        { type: "meta", id: "123", token: "tok", total: 2 },
        { type: "page", page_url: "https://exhentai.org/s/a/1", index: 0 },
      ),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream ended unexpectedly",
    );
  });

  it("rejects when fewer pages arrive than the meta total promised", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(
        { type: "meta", id: "123", token: "tok", total: 2 },
        { type: "page", page_url: "https://exhentai.org/s/a/1", index: 0 },
        { type: "done", total: 1 },
      ),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream ended with 1 of 2 pages",
    );
  });
});

describe("fetchGalleryPagesWithFallback", () => {
  it("uses the live endpoint when it succeeds", async () => {
    fetchMock.mockResolvedValue(liveStream());

    const result = await fetchGalleryPagesWithFallback(123, "tok");

    expect(result.pages).toHaveLength(2);
    expect(result.total).toBe(2);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe(`${BASE}/api/gallery/123/tok/pages`);
  });

  it("falls back to the cached page list when the live endpoint fails", async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ error: "upstream down" }, 502))
      .mockResolvedValueOnce(jsonResponse(pages));

    const result = await fetchGalleryPagesWithFallback(123, "tok");

    expect(result).toEqual(pages);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[1][0]).toBe(
      `${BASE}/api/gallery-cache/123/tok/pages`,
    );
  });

  it("discards partial pages and falls back when the stream fails mid-way", async () => {
    fetchMock
      .mockResolvedValueOnce(
        ndjsonResponse(
          { type: "meta", id: "123", token: "tok", total: 65 },
          { type: "page", page_url: "https://exhentai.org/s/a/1", index: 0 },
          { type: "error", error: "fetch gallery pages failed: gave up" },
        ),
      )
      .mockResolvedValueOnce(jsonResponse(pages));

    const result = await fetchGalleryPagesWithFallback(123, "tok");

    expect(result).toEqual(pages);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[1][0]).toBe(
      `${BASE}/api/gallery-cache/123/tok/pages`,
    );
  });
});
