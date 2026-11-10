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

const listResponse = () =>
  jsonResponse({ page: 0, page_size: 0, results: [] });
const searchResponse = () =>
  jsonResponse({
    total: 0,
    total_pages: 0,
    page: 0,
    page_size: 0,
    results: [],
  });

describe("listing parameters", () => {
  it("forwards jump/seek to the matching endpoint", async () => {
    const cases = [
      {
        name: "galleries jump",
        call: () => fetchGalleries(0, undefined, { jump: "1y" }),
        expected: { jump: "1y" },
        absent: "seek",
      },
      {
        name: "watched seek",
        call: () => fetchWatched(0, undefined, { seek: "2020-01" }),
        expected: { seek: "2020-01" },
        absent: "jump",
      },
      {
        name: "search jump",
        call: () => fetchSearch({ q: "yuri", jump: "1m" }),
        expected: { q: "yuri", jump: "1m" },
        absent: "seek",
      },
    ];

    for (const tc of cases) {
      fetchMock.mockResolvedValue(searchResponse());
      await tc.call();

      const url = lastUrl();
      for (const [key, value] of Object.entries(tc.expected)) {
        expect(url.searchParams.get(key), `${tc.name}: ${key}`).toBe(value);
      }
      expect(url.searchParams.has(tc.absent), `${tc.name}: ${tc.absent}`).toBe(
        false,
      );
    }
  });

  it("omits jump/seek when not provided", async () => {
    fetchMock.mockResolvedValue(listResponse());

    await fetchGalleries(0);

    const url = lastUrl();
    expect(url.searchParams.has("seek")).toBe(false);
    expect(url.searchParams.has("jump")).toBe(false);
  });

  it("forwards tags and categories and joins list values", async () => {
    const cases = [
      {
        name: "galleries tags",
        call: () => fetchGalleries(0, { tags: ["female:yuri", "full color"] }),
        param: "tags",
        value: "female:yuri,full color",
      },
      {
        name: "watched tags",
        call: () => fetchWatched(0, { tags: ["lolicon"] }),
        param: "tags",
        value: "lolicon",
      },
      {
        name: "galleries categories",
        call: () => fetchGalleries(0, { categories: ["doujinshi", "manga"] }),
        param: "categories",
        value: "doujinshi,manga",
      },
      {
        name: "watched categories",
        call: () => fetchWatched(0, { categories: ["misc"] }),
        param: "categories",
        value: "misc",
      },
    ];

    for (const tc of cases) {
      fetchMock.mockResolvedValue(listResponse());
      await tc.call();
      expect(lastUrl().searchParams.get(tc.param), tc.name).toBe(tc.value);
    }
  });

  it("omits empty tag and category lists", async () => {
    fetchMock.mockResolvedValue(listResponse());

    await fetchGalleries(0, { tags: [], categories: [] });

    const url = lastUrl();
    expect(url.searchParams.has("tags")).toBe(false);
    expect(url.searchParams.has("categories")).toBe(false);
  });
});
