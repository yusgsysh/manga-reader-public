import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchGalleries, fetchWatched } from "./gallery";
import { fetchSearch } from "./search";

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

function lastUrl(): URL {
  return new URL(fetchMock.mock.calls.at(-1)![0] as string);
}

describe("listing jump/seek parameters", () => {
  it("forwards jump on the homepage list", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchGalleries(0, undefined, { jump: "1y" });

    const url = lastUrl();
    expect(url.searchParams.get("jump")).toBe("1y");
    expect(url.searchParams.has("seek")).toBe(false);
  });

  it("forwards seek on the watched list", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchWatched(0, undefined, { seek: "2020-01" });

    const url = lastUrl();
    expect(url.searchParams.get("seek")).toBe("2020-01");
    expect(url.searchParams.has("jump")).toBe(false);
  });

  it("forwards seek on search", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ total: 0, total_pages: 0, page: 0, page_size: 0, results: [] }),
    );

    await fetchSearch({ q: "yuri", jump: "1m" });

    const url = lastUrl();
    expect(url.searchParams.get("q")).toBe("yuri");
    expect(url.searchParams.get("jump")).toBe("1m");
  });

  it("omits nav parameters when not provided", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchGalleries(0);

    const url = lastUrl();
    expect(url.searchParams.has("seek")).toBe(false);
    expect(url.searchParams.has("jump")).toBe(false);
  });
});
