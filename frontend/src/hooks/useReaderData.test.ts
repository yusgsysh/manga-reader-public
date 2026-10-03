import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import type { InfiniteData } from "@tanstack/react-query";
import {
  applyReadingProgressToCaches,
  invalidateReadingLists,
} from "./useReaderData";
import type { Gallery } from "../types/reader";
import type { ReadingProgress } from "../types/reader";
import type {
  BookshelfItem,
  BookshelfListResponse,
} from "../types/gallery";
import type {
  RecentlyReadItem,
  RecentlyReadResponse,
} from "../types/recentlyRead";

function progressFor(id: number, updatedAt: string): ReadingProgress {
  return {
    gallery_id: id,
    token: `token-${id}`,
    current_page: 9,
    progress: 1,
    completed: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: updatedAt,
  };
}

function item(id: number, updatedAt: string): RecentlyReadItem {
  return {
    id,
    token: `token-${id}`,
    title: `Title ${id}`,
    title_jpn: "",
    category: "doujinshi",
    thumbnail: `thumb-${id}`,
    pages: 10,
    reading: {
      gallery_id: id,
      token: `token-${id}`,
      current_page: 1,
      progress: 0.5,
      completed: false,
      created_at: updatedAt,
      updated_at: updatedAt,
    },
  };
}

function emptyPage(page: number): RecentlyReadResponse {
  return { page, page_size: 25, total: 0, total_pages: 0, results: [] };
}

function infinite(
  results: RecentlyReadItem[],
): InfiniteData<RecentlyReadResponse> {
  return {
    pages: [
      { page: 0, page_size: 25, total: results.length, total_pages: 1, results },
    ],
    pageParams: [0],
  };
}

function shelfItem(id: number, updatedAt: string): BookshelfItem {
  return {
    id,
    token: `token-${id}`,
    title: `Title ${id}`,
    title_jpn: "",
    category: "doujinshi",
    thumbnail: `thumb-${id}`,
    pages: 10,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: updatedAt,
    reading: {
      gallery_id: id,
      token: `token-${id}`,
      current_page: 1,
      progress: 0.5,
      completed: false,
      created_at: updatedAt,
      updated_at: updatedAt,
    },
  };
}

function infiniteShelf(
  results: BookshelfItem[],
): InfiniteData<BookshelfListResponse> {
  return {
    pages: [
      { page: 0, page_size: 25, total: results.length, total_pages: 1, results },
    ],
    pageParams: [0],
  };
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

  it("moves the just-read gallery to the front of recently-read", () => {
    const client = new QueryClient();
    client.setQueryData(
      ["recently-read"],
      infinite([
        item(1, "2026-01-03T00:00:00Z"),
        item(2, "2026-01-02T00:00:00Z"),
        item(3, "2026-01-01T00:00:00Z"),
      ]),
    );

    applyReadingProgressToCaches(
      client,
      2,
      "token-2",
      progressFor(2, "2026-01-04T00:00:00Z"),
    );

    const data = client.getQueryData<InfiniteData<RecentlyReadResponse>>([
      "recently-read",
    ])!;
    expect(data.pages[0].results.map((r) => r.id)).toEqual([2, 1, 3]);

    const moved = data.pages[0].results[0];
    expect(moved.reading).toMatchObject({
      current_page: 9,
      completed: true,
      updated_at: "2026-01-04T00:00:00Z",
    });
    // Enrichment metadata is preserved.
    expect(moved.title).toBe("Title 2");
    expect(moved.thumbnail).toBe("thumb-2");
  });

  it("prepends a gallery not yet in the recently-read list", () => {
    const client = new QueryClient();
    client.setQueryData(
      ["recently-read"],
      infinite([item(1, "2026-01-03T00:00:00Z")]),
    );
    client.setQueryData<Gallery>(["gallery", 9, "token-9"], {
      id: 9,
      token: "token-9",
      title: "Gallery 9",
      title_jpn: "",
      category: "manga",
      thumbnail: "thumb-9",
      page_count: 42,
      rating: 0,
      rating_count: 0,
      uploader: "",
      tags: [],
    });

    applyReadingProgressToCaches(
      client,
      9,
      "token-9",
      progressFor(9, "2026-01-04T00:00:00Z"),
    );

    const data = client.getQueryData<InfiniteData<RecentlyReadResponse>>([
      "recently-read",
    ])!;
    expect(data.pages[0].results.map((r) => r.id)).toEqual([9, 1]);
    const first = data.pages[0].results[0];
    expect(first.title).toBe("Gallery 9");
    expect(first.pages).toBe(42);
  });

  it("does nothing when there is no recently-read cache", () => {
    const client = new QueryClient();

    expect(() =>
      applyReadingProgressToCaches(
        client,
        1,
        "token-1",
        progressFor(1, "2026-01-04T00:00:00Z"),
      ),
    ).not.toThrow();

    expect(client.getQueryData(["recently-read"])).toBeUndefined();
  });
});

describe("applyReadingProgressToCaches bookshelf reorder", () => {
  it("moves a shelved gallery to the front of the bookshelf", () => {
    const client = new QueryClient();
    client.setQueryData(
      ["bookshelf"],
      infiniteShelf([
        shelfItem(1, "2026-01-03T00:00:00Z"),
        shelfItem(2, "2026-01-02T00:00:00Z"),
        shelfItem(3, "2026-01-01T00:00:00Z"),
      ]),
    );

    applyReadingProgressToCaches(
      client,
      2,
      "token-2",
      progressFor(2, "2026-01-04T00:00:00Z"),
    );

    const data = client.getQueryData<InfiniteData<BookshelfListResponse>>([
      "bookshelf",
    ])!;
    expect(data.pages[0].results.map((r) => r.id)).toEqual([2, 1, 3]);

    const moved = data.pages[0].results[0];
    expect(moved.reading).toMatchObject({
      current_page: 9,
      completed: true,
      updated_at: "2026-01-04T00:00:00Z",
    });
    // Metadata is preserved and the shelf updated_at tracks the read.
    expect(moved.title).toBe("Title 2");
    expect(moved.thumbnail).toBe("thumb-2");
    expect(moved.updated_at).toBe("2026-01-04T00:00:00Z");
  });

  it("leaves the bookshelf untouched when the gallery is not shelved", () => {
    const client = new QueryClient();
    client.setQueryData(
      ["bookshelf"],
      infiniteShelf([
        shelfItem(1, "2026-01-03T00:00:00Z"),
        shelfItem(3, "2026-01-02T00:00:00Z"),
      ]),
    );

    applyReadingProgressToCaches(
      client,
      9,
      "token-9",
      progressFor(9, "2026-01-04T00:00:00Z"),
    );

    const data = client.getQueryData<InfiniteData<BookshelfListResponse>>([
      "bookshelf",
    ])!;
    expect(data.pages[0].results.map((r) => r.id)).toEqual([1, 3]);
  });

  it("does nothing when there is no bookshelf cache", () => {
    const client = new QueryClient();

    expect(() =>
      applyReadingProgressToCaches(
        client,
        1,
        "token-1",
        progressFor(1, "2026-01-04T00:00:00Z"),
      ),
    ).not.toThrow();

    expect(client.getQueryData(["bookshelf"])).toBeUndefined();
  });
});

describe("invalidateReadingLists", () => {
  it("invalidates every cached recently-read page", () => {
    const client = new QueryClient();
    client.setQueryData<RecentlyReadResponse>(["recently-read", 0], emptyPage(0));
    client.setQueryData<RecentlyReadResponse>(["recently-read", 1], emptyPage(1));
    client.setQueryData(["bookshelf"], emptyPage(0));

    invalidateReadingLists(client);

    expect(client.getQueryState(["recently-read", 0])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["recently-read", 1])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["bookshelf"])?.isInvalidated).toBe(true);
  });
});
