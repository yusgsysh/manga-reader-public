import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchGalleryPages, fetchGalleryPagesWithFallback } from "./gallery";
import type { GalleryPagesResponse } from "../types/reader";

const BASE = "http://localhost:8080";

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

function meta(total: number) {
  return { type: "meta", id: "123", token: "tok", total };
}

function page(index: number) {
  return {
    type: "page",
    page_url: `https://exhentai.org/s/a/${index + 1}`,
    index,
  };
}

// Aggregated page objects drop the stream-only `type` field.
function expectedPage(index: number) {
  return {
    page_url: `https://exhentai.org/s/a/${index + 1}`,
    index,
    thumbnail: undefined,
  };
}

function done(total: number) {
  return { type: "done", total };
}

// meta(total=3) → page×3 → done(total=3)
function liveStream(): Response {
  return ndjsonResponse(meta(3), page(0), page(1), page(2), done(3));
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
      total: 3,
      pages: [expectedPage(0), expectedPage(1), expectedPage(2)],
    });

    // total always mirrors meta.total (the gallery total), while pages grows.
    expect(snapshots.map((s) => s.total)).toEqual([3, 3, 3]);
    expect(snapshots.map((s) => s.pages.length)).toEqual([1, 2, 3]);
    expect(snapshots[0].id).toBe("123");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe(
      `${BASE}/api/gallery/123/tok/pages`,
    );
  });

  it("rejects a duplicate meta line", async () => {
    fetchMock.mockResolvedValue(ndjsonResponse(meta(3), meta(3)));

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "duplicate gallery pages stream meta",
    );
  });

  it("rejects a page before meta", async () => {
    fetchMock.mockResolvedValue(ndjsonResponse(page(0), meta(3)));

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream sent a page before meta",
    );
  });

  it("rejects a page index gap", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(meta(3), page(0), page(2), done(3)),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream page index mismatch: expected 1, got 2",
    );
  });

  it("rejects a duplicate page index", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(meta(3), page(0), page(0), done(3)),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream page index mismatch: expected 1, got 0",
    );
  });

  it("rejects a done total mismatch", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(meta(3), page(0), page(1), done(1)),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream done total mismatch: 1 vs 2",
    );
  });

  it("rejects an incomplete stream", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(meta(3), page(0), page(1), done(2)),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream ended with 2 of 3 pages",
    );
  });

  it("rejects when the stream reports an error line", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(meta(3), page(0), {
        type: "error",
        error: "fetch gallery pages failed: upstream broke",
      }),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "upstream broke",
    );
  });

  it("rejects when the stream ends without a terminal line", async () => {
    fetchMock.mockResolvedValue(ndjsonResponse(meta(3), page(0)));

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream ended unexpectedly",
    );
  });

  it("rejects invalid JSON lines without swallowing the error", async () => {
    fetchMock.mockResolvedValue(
      new Response(`${JSON.stringify(meta(3))}\nnot-json\n`, {
        status: 200,
        headers: { "Content-Type": "application/x-ndjson" },
      }),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "invalid gallery pages stream line: not-json",
    );
  });

  it("reports progressive snapshots before the stream completes", async () => {
    let controller!: ReadableStreamDefaultController<Uint8Array>;
    const encoder = new TextEncoder();
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        controller = c;
        c.enqueue(encoder.encode(`${JSON.stringify(meta(3))}\n`));
        c.enqueue(encoder.encode(`${JSON.stringify(page(0))}\n`));
      },
    });
    fetchMock.mockResolvedValue(
      new Response(stream, {
        status: 200,
        headers: { "Content-Type": "application/x-ndjson" },
      }),
    );

    const snapshots: number[] = [];
    const promise = fetchGalleryPages(123, "tok", (s) =>
      snapshots.push(s.pages.length),
    );

    // The first page must reach the caller while the stream is still open.
    await vi.waitFor(() => expect(snapshots).toEqual([1]));

    controller.enqueue(encoder.encode(`${JSON.stringify(page(1))}\n`));
    controller.enqueue(encoder.encode(`${JSON.stringify(page(2))}\n`));
    controller.enqueue(encoder.encode(`${JSON.stringify(done(3))}\n`));
    controller.close();

    const result = await promise;
    expect(result.pages).toHaveLength(3);
    expect(snapshots).toEqual([1, 2, 3]);
  });

  it("rejects when the stream exceeds meta.total", async () => {
    fetchMock.mockResolvedValue(
      ndjsonResponse(meta(1), page(0), page(1), done(2)),
    );

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "gallery pages stream exceeded meta total: 2 > 1",
    );
  });

  it("forwards the abort signal to the request", async () => {
    fetchMock.mockResolvedValue(liveStream());
    const controller = new AbortController();

    await fetchGalleryPages(123, "tok", undefined, controller.signal);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][1]?.signal).toBe(controller.signal);
  });

  it("releases the reader lock and cancels the stream on failure", async () => {
    const res = ndjsonResponse(meta(3), page(0), page(0), done(3));
    fetchMock.mockResolvedValue(res);

    await expect(fetchGalleryPages(123, "tok")).rejects.toThrow(
      "page index mismatch",
    );

    expect(res.body).not.toBeNull();
    expect(res.body!.locked).toBe(false);
    // cancel() closed the stream, so nothing remains readable after failure.
    const { done: streamDone } = await res.body!.getReader().read();
    expect(streamDone).toBe(true);
  });
});

describe("fetchGalleryPagesWithFallback", () => {
  it("uses the live endpoint when it succeeds", async () => {
    fetchMock.mockResolvedValue(liveStream());

    const result = await fetchGalleryPagesWithFallback(123, "tok");

    expect(result).toEqual({
      id: "123",
      token: "tok",
      total: 3,
      pages: [expectedPage(0), expectedPage(1), expectedPage(2)],
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe(`${BASE}/api/gallery/123/tok/pages`);
  });

  it("falls back to the cached page list when the live endpoint fails", async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ error: "upstream down" }, 502))
      .mockResolvedValueOnce(ndjsonResponse(meta(1), page(0), done(1)));

    const result = await fetchGalleryPagesWithFallback(123, "tok");

    expect(result).toEqual({
      id: "123",
      token: "tok",
      total: 1,
      pages: [expectedPage(0)],
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[1][0]).toBe(
      `${BASE}/api/gallery-cache/123/tok/pages`,
    );
  });

  it("discards partial pages and falls back when the stream fails mid-way", async () => {
    fetchMock
      .mockResolvedValueOnce(
        ndjsonResponse(
          meta(65),
          page(0),
          { type: "error", error: "fetch gallery pages failed: gave up" },
        ),
      )
      .mockResolvedValueOnce(ndjsonResponse(meta(1), page(0), done(1)));

    const result = await fetchGalleryPagesWithFallback(123, "tok");

    expect(result).toEqual({
      id: "123",
      token: "tok",
      total: 1,
      pages: [expectedPage(0)],
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[1][0]).toBe(
      `${BASE}/api/gallery-cache/123/tok/pages`,
    );
  });
});
