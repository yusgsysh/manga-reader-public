import { apiGet } from "./client";
import type {
  AdvancedSearchOptions,
  GalleryDetail,
  GalleryListResponse,
  ListingNavOptions,
} from "../types/gallery";
import type { Gallery, GalleryPagesResponse } from "../types/reader";

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

export function fetchGalleryPages(
  id: number,
  token: string,
): Promise<GalleryPagesResponse> {
  return apiGet<GalleryPagesResponse>(`/api/gallery/${id}/${token}/pages`);
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
