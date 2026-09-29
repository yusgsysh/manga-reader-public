import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  applyReadingProgressToCaches,
  invalidateReadingLists,
} from "./useReaderData";
import type { ReadingProgress } from "../types/reader";
import type { RecentlyReadResponse } from "../types/recentlyRead";

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

function emptyPage(page: number): RecentlyReadResponse {
  return { page, page_size: 25, total: 0, total_pages: 0, results: [] };
}

describe("applyReadingProgressToCaches", () => {
  it("writes the progress into the reading-progress cache", () => {
    const client = new QueryClient();

    applyReadingProgressToCaches(
      client,
      2,
      "token-2",
      progressFor(2, "2026-01-04T00:00:00Z"),
    );

    expect(
      client.getQueryData<ReadingProgress>(["reading-progress", 2, "token-2"]),
    ).toMatchObject({ current_page: 9, completed: true });
  });

  it("leaves every recently-read page untouched", () => {
    const client = new QueryClient();
    client.setQueryData<RecentlyReadResponse>(["recently-read", 0], emptyPage(0));
    client.setQueryData<RecentlyReadResponse>(["recently-read", 1], emptyPage(1));

    applyReadingProgressToCaches(
      client,
      1,
      "token-1",
      progressFor(1, "2026-01-04T00:00:00Z"),
    );

    expect(client.getQueryState(["recently-read", 0])?.isInvalidated).toBe(false);
    expect(client.getQueryState(["recently-read", 1])?.isInvalidated).toBe(false);
    expect(
      client.getQueryData<RecentlyReadResponse>(["recently-read", 0])!.results,
    ).toEqual([]);
  });
});

describe("invalidateReadingLists", () => {
  it("invalidates every cached recently-read page", () => {
    const client = new QueryClient();
    client.setQueryData<RecentlyReadResponse>(["recently-read", 0], emptyPage(0));
    client.setQueryData<RecentlyReadResponse>(["recently-read", 1], emptyPage(1));
    client.setQueryData(["bookshelf", 0], emptyPage(0));

    invalidateReadingLists(client);

    expect(client.getQueryState(["recently-read", 0])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["recently-read", 1])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["bookshelf", 0])?.isInvalidated).toBe(true);
  });
});
