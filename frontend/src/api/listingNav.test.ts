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

  it("forwards tags on the homepage list", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchGalleries(0, { tags: ["female:yuri", "full color"] });

    const url = lastUrl();
    expect(url.searchParams.get("tags")).toBe("female:yuri,full color");
  });

  it("forwards tags on the watched list", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchWatched(0, { tags: ["lolicon"] });

    const url = lastUrl();
    expect(url.searchParams.get("tags")).toBe("lolicon");
  });

  it("omits tags when the list is empty", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchGalleries(0, { tags: [] });

    const url = lastUrl();
    expect(url.searchParams.has("tags")).toBe(false);
  });

  it("forwards categories on the homepage list", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchGalleries(0, { categories: ["doujinshi", "manga"] });

    const url = lastUrl();
    expect(url.searchParams.get("categories")).toBe("doujinshi,manga");
  });

  it("forwards categories on the watched list", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchWatched(0, { categories: ["misc"] });

    const url = lastUrl();
    expect(url.searchParams.get("categories")).toBe("misc");
  });

  it("omits categories when the list is empty", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ page: 0, page_size: 0, results: [] }),
    );

    await fetchGalleries(0, { categories: [] });

    const url = lastUrl();
    expect(url.searchParams.has("categories")).toBe(false);
  });
});
