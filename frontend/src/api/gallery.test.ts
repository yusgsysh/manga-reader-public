import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchGalleryPagesWithFallback } from "./gallery";
import type { GalleryPagesResponse } from "../types/reader";

const BASE = "http://localhost:8080";

const pages: GalleryPagesResponse = {
  id: "123",
  token: "tok",
  total: 1,
  pages: [{ page_url: "https://exhentai.org/s/a/1", index: 0 }],
};

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("fetchGalleryPagesWithFallback", () => {
  it("uses the live endpoint when it succeeds", async () => {
    fetchMock.mockResolvedValue(jsonResponse(pages));

    const result = await fetchGalleryPagesWithFallback(123, "tok");

    expect(result).toEqual(pages);
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
});
