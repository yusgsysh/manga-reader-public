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

// fetchGalleryPages reads the streaming NDJSON page list: a meta line, one
// line per page, then a terminal done/error line. onPage receives a snapshot
// after every page so callers can render progressively; the resolved promise
// carries the aggregated response. The stream fails (throws) on the error
// line or when it ends without a terminal line, so partial data is never
// mistaken for success.
export async function fetchGalleryPages(
  id: number,
  token: string,
  onPage?: (partial: GalleryPagesResponse) => void,
): Promise<GalleryPagesResponse> {
  const res = await fetchChecked(
    buildApiUrl(`/api/gallery/${id}/${token}/pages`),
  );
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
    switch (event.type) {
      case "meta":
        metaId = event.id;
        metaToken = event.token;
        total = event.total;
        sawMeta = true;
        break;
      case "page":
        if (!sawMeta) {
          throw new Error("gallery pages stream sent a page before meta");
        }
        pages.push({
          page_url: event.page_url,
          index: event.index,
          thumbnail: event.thumbnail,
        });
        onPage?.({
          id: metaId,
          token: metaToken,
          total: pages.length,
          pages: [...pages],
        });
        break;
      case "done":
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

  return { id: metaId, token: metaToken, total: pages.length, pages };
}

export function fetchGalleryDetail(
  id: number,
  token: string,
): Promise<GalleryDetail> {
  return apiGet<GalleryDetail>(`/api/gallery/${id}/${token}/details`);
}

// Offline cache endpoints (read-only). Used by the frontend as a fallback when
// the online endpoints above fail.

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

export function fetchGalleryPagesCached(
  id: number,
  token: string,
): Promise<GalleryPagesResponse> {
  return apiGet<GalleryPagesResponse>(
    `/api/gallery-cache/${id}/${token}/pages`,
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
