import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  applyReadingProgressToCaches,
  updateRecentlyReadCache,
} from "./useReaderData";
import type { ReadingProgress } from "../types/reader";
import type { RecentlyReadItem, RecentlyReadResponse } from "../types/recentlyRead";

function item(id: number, updatedAt: string): RecentlyReadItem {
  return {
    id,
    token: `token-${id}`,
    title: `Book ${id}`,
    title_jpn: "",
    category: "manga",
    thumbnail: "",
    pages: 10,
    reading: {
      gallery_id: id,
      token: `token-${id}`,
      current_page: 1,
      progress: 0.1,
      completed: false,
      updated_at: updatedAt,
    },
  };
}

function progressFor(id: number, updatedAt: string): ReadingProgress {
  return {
    gallery_id: id,
    token: `token-${id}`,
    current_page: 9,
    progress: 1,
    completed: true,
    started_at: "2026-01-01T00:00:00Z",
    updated_at: updatedAt,
  };
}

function clientWith(results: RecentlyReadItem[]): QueryClient {
  const client = new QueryClient();
  client.setQueryData<RecentlyReadResponse>(["recently-read"], { results });
  return client;
}

function resultsOf(client: QueryClient): RecentlyReadItem[] {
  return client.getQueryData<RecentlyReadResponse>(["recently-read"])!.results;
}

describe("updateRecentlyReadCache", () => {
  it("moves the freshly read book to the front", () => {
    const client = clientWith([
      item(1, "2026-01-03T00:00:00Z"),
      item(2, "2026-01-02T00:00:00Z"),
      item(3, "2026-01-01T00:00:00Z"),
    ]);

    updateRecentlyReadCache(client, 3, "token-3", progressFor(3, "2026-01-04T00:00:00Z"));

    const results = resultsOf(client);
    expect(results.map((entry) => entry.id)).toEqual([3, 1, 2]);
    expect(results[0].reading).toMatchObject({
      current_page: 9,
      progress: 1,
      completed: true,
      updated_at: "2026-01-04T00:00:00Z",
    });
  });

  it("inserts a book that is missing from the cached list", () => {
    const client = clientWith([item(1, "2026-01-03T00:00:00Z")]);
    client.setQueryData(["gallery", 9, "token-9"], {
      id: 9,
      token: "token-9",
      title: "Fresh",
      title_jpn: "",
      category: "manga",
      thumbnail: "thumb.webp",
      page_count: 12,
      rating: 4.5,
      rating_count: 10,
      uploader: "someone",
      tags: [],
    });

    updateRecentlyReadCache(client, 9, "token-9", progressFor(9, "2026-01-04T00:00:00Z"));

    const results = resultsOf(client);
    expect(results.map((entry) => entry.id)).toEqual([9, 1]);
    expect(results[0]).toMatchObject({
      title: "Fresh",
      thumbnail: "thumb.webp",
      pages: 12,
      reading: { completed: true },
    });
  });

  it("keeps the list capped at the server page size", () => {
    const many = Array.from({ length: 25 }, (_unused, index) =>
      item(100 + index, `2026-01-01T00:00:${String(index).padStart(2, "0")}Z`),
    );
    const client = clientWith(many);

    updateRecentlyReadCache(client, 999, "token-999", progressFor(999, "2026-02-01T00:00:00Z"));

    const results = resultsOf(client);
    expect(results).toHaveLength(25);
    expect(results[0].id).toBe(999);
    expect(results[24].id).toBe(101);
  });

  it("leaves the cache untouched when the list was never loaded", () => {
    const client = new QueryClient();

    updateRecentlyReadCache(client, 1, "token-1", progressFor(1, "2026-01-04T00:00:00Z"));

    expect(client.getQueryData(["recently-read"])).toBeUndefined();
  });
});

describe("applyReadingProgressToCaches", () => {
  it("writes the progress and reorders the list", () => {
    const client = clientWith([
      item(1, "2026-01-03T00:00:00Z"),
      item(2, "2026-01-02T00:00:00Z"),
    ]);

    applyReadingProgressToCaches(client, 2, "token-2", progressFor(2, "2026-01-04T00:00:00Z"));

    expect(
      client.getQueryData<ReadingProgress>(["reading-progress", 2, "token-2"]),
    ).toMatchObject({ current_page: 9, completed: true });
    expect(resultsOf(client).map((entry) => entry.id)).toEqual([2, 1]);
  });
});
