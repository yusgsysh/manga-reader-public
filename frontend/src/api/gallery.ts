import { buildApiUrl, fetchChecked, apiGet } from "./client";
import type {
  AdvancedSearchOptions,
  GalleryDetail,
  GalleryListResponse,
  ListingNavOptions,
} from "../types/gallery";
import type {
  Gallery,
  GalleryPage,
  GalleryPagesResponse,
  GalleryPagesStreamEvent,
} from "../types/reader";

function boolToStr(v?: boolean): string | undefined {
  return v === undefined ? undefined : v ? "true" : "false";
}

function advancedToParams(filters?: AdvancedSearchOptions) {
  if (!filters) return {};
  return {
    min_pages: filters.min_pages,
    max_pages: filters.max_pages,
    min_rating: filters.min_rating,
    has_torrent: boolToStr(filters.has_torrent),
    include_expunged: boolToStr(filters.include_expunged),
    search_name: boolToStr(filters.search_name),
    search_tags: boolToStr(filters.search_tags),
    search_description: boolToStr(filters.search_description),
    include_low_power_tags: boolToStr(filters.include_low_power_tags),
    include_downvoted_tags: boolToStr(filters.include_downvoted_tags),
    disable_language_filter: boolToStr(filters.disable_language_filter),
    disable_uploader_filter: boolToStr(filters.disable_uploader_filter),
    disable_tag_filter: boolToStr(filters.disable_tag_filter),
  };
}

function navToParams(nav?: ListingNavOptions) {
  if (!nav) return {};
  return { seek: nav.seek, jump: nav.jump };
}

export function fetchGalleries(
  page: number,
  filters?: AdvancedSearchOptions,
  nav?: ListingNavOptions,
): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/galleries", {
    page,
    ...advancedToParams(filters),
    ...navToParams(nav),
  });
}

export function fetchWatched(
  page: number,
  filters?: AdvancedSearchOptions,
  nav?: ListingNavOptions,
): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/watched", {
    page,
    ...advancedToParams(filters),
    ...navToParams(nav),
  });
}

export function fetchPopular(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/popular", { page });
}

export function fetchGallery(id: number, token: string): Promise<Gallery> {
  return apiGet<Gallery>(`/api/gallery/${id}/${token}`);
}

// fetchGalleryPages reads the streaming NDJSON page list from the online
// endpoint. The online endpoint always scrapes upstream and never writes cache.
export function fetchGalleryPages(
  id: number,
  token: string,
  onPage?: (partial: GalleryPagesResponse) => void,
  signal?: AbortSignal,
): Promise<GalleryPagesResponse> {
  return readGalleryPagesStream(
    buildApiUrl(`/api/gallery/${id}/${token}/pages`),
    id,
    token,
    onPage,
    signal,
  );
}

// readGalleryPagesStream parses a streaming NDJSON page list. The protocol state
// machine is meta → page* → (done | error):
//   - meta appears exactly once and must come first; its `total` is the
//     gallery's declared page count and stays constant for the whole stream;
//   - page indices are contiguous from 0;
//   - done.total must equal the number of received pages (and, when the
//     gallery total is known, the received count must cover it);
//   - error terminates the stream as a failure.
// onPage receives a snapshot after every page (total = gallery total,
// pages = received so far) so callers can render progressively; the resolved
// promise carries the aggregated response. The stream fails (throws) on the
// error line or when it ends without a terminal line, so partial data is
// never mistaken for success. Shared by the online and read-through cache
// page endpoints, which return the same format.
async function readGalleryPagesStream(
  url: string,
  id: number,
  token: string,
  onPage?: (partial: GalleryPagesResponse) => void,
  signal?: AbortSignal,
): Promise<GalleryPagesResponse> {
  const res = await fetchChecked(url, signal ? { signal } : undefined);
  if (!res.body) {
    throw new Error("gallery pages stream has no body");
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  const pages: GalleryPage[] = [];
  let buffer = "";
  let metaId = String(id);
  let metaToken = token;
  let total = 0;
  let sawMeta = false;
  let finished = false;

  const handleLine = (line: string) => {
    if (!line.trim()) return;
    let event: GalleryPagesStreamEvent;
    try {
      event = JSON.parse(line) as GalleryPagesStreamEvent;
    } catch {
      throw new Error(`invalid gallery pages stream line: ${line}`);
    }

    if (finished) {
      throw new Error(`gallery pages stream line after done: ${line}`);
    }

    switch (event.type) {
      case "meta":
        if (sawMeta) {
          throw new Error("duplicate gallery pages stream meta");
        }
        metaId = event.id;
        metaToken = event.token;
        total = event.total;
        sawMeta = true;
        break;
      case "page":
        if (!sawMeta) {
          throw new Error("gallery pages stream sent a page before meta");
        }
        if (event.index !== pages.length) {
          throw new Error(
            `gallery pages stream page index mismatch: expected ${pages.length}, got ${event.index}`,
          );
        }
        pages.push({
          page_url: event.page_url,
          index: event.index,
          thumbnail: event.thumbnail,
        });
        if (total > 0 && pages.length > total) {
          throw new Error(
            `gallery pages stream exceeded meta total: ${pages.length} > ${total}`,
          );
        }
        onPage?.({
          id: metaId,
          token: metaToken,
          total,
          pages: [...pages],
        });
        break;
      case "done":
        if (!sawMeta) {
          throw new Error("gallery pages stream sent done before meta");
        }
        if (event.total !== pages.length) {
          throw new Error(
            `gallery pages stream done total mismatch: ${event.total} vs ${pages.length}`,
          );
        }
        if (total > 0 && pages.length < total) {
          throw new Error(
            `gallery pages stream ended with ${pages.length} of ${total} pages`,
          );
        }
        finished = true;
        break;
      case "error":
        throw new Error(event.error);
      default:
        throw new Error(`unknown gallery pages stream line: ${line}`);
    }
  };

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let newline = buffer.indexOf("\n");
      while (newline >= 0) {
        handleLine(buffer.slice(0, newline));
        buffer = buffer.slice(newline + 1);
        newline = buffer.indexOf("\n");
      }
    }
    buffer += decoder.decode();
    if (buffer.trim()) handleLine(buffer.trim());

    if (!sawMeta) {
      throw new Error("gallery pages stream ended without meta");
    }
    if (!finished) {
      throw new Error("gallery pages stream ended unexpectedly");
    }
  } catch (error) {
    // Abort the underlying stream on any failure (protocol, JSON, network)
    // without letting a rejected cancel() replace the original error.
    await reader.cancel(error).catch(() => undefined);
    throw error;
  } finally {
    reader.releaseLock();
  }

  // total mirrors meta.total (the gallery total); verification above guarantees
  // it equals pages.length when meta.total is positive.
  return { id: metaId, token: metaToken, total: total > 0 ? total : pages.length, pages };
}

export function fetchGalleryDetail(
  id: number,
  token: string,
): Promise<GalleryDetail> {
  return apiGet<GalleryDetail>(`/api/gallery/${id}/${token}/details`);
}

// Cache endpoints (read-through). A hit is served from the backend cache; a
// miss makes the backend fetch upstream and backfill.

export function fetchGalleryCached(
  id: number,
  token: string,
): Promise<Gallery> {
  return apiGet<Gallery>(`/api/gallery-cache/${id}/${token}`);
}

export function fetchGalleryDetailCached(
  id: number,
  token: string,
): Promise<GalleryDetail> {
  return apiGet<GalleryDetail>(`/api/gallery-cache/${id}/${token}/details`);
}

// The cached /pages endpoint streams the same NDJSON format as the online one:
// a hit replays the cached list, a miss reads through upstream and backfills.
export function fetchGalleryPagesCached(
  id: number,
  token: string,
  onPage?: (partial: GalleryPagesResponse) => void,
  signal?: AbortSignal,
): Promise<GalleryPagesResponse> {
  return readGalleryPagesStream(
    buildApiUrl(`/api/gallery-cache/${id}/${token}/pages`),
    id,
    token,
    onPage,
    signal,
  );
}

// Fetches the page list preferring the live endpoint, falling back to the
// cached page list when the browser is offline or ExHentai is unreachable. Used
// by the download flow so a job can still be queued from cached pages.
export async function fetchGalleryPagesWithFallback(
  id: number,
  token: string,
): Promise<GalleryPagesResponse> {
  if (typeof navigator !== "undefined" && navigator.onLine === false) {
    return fetchGalleryPagesCached(id, token);
  }
  try {
    return await fetchGalleryPages(id, token);
  } catch {
    return fetchGalleryPagesCached(id, token);
  }
}
