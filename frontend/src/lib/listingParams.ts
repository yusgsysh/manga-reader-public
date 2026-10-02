import {
  parseAdvancedParams,
  pickAdvancedOptions,
  serializeAdvancedParams,
} from "../components/search/advancedSearch";
import type {
  GalleryListFilters,
  ListingNavOptions,
} from "../types/gallery";

// URL keys that make up a listing's filter state. Kept in sync with
// advancedToParams in api/gallery.ts so a pasted URL reproduces the request.
const LISTING_FILTER_KEYS = [
  "tags",
  "categories",
  "min_pages",
  "max_pages",
  "min_rating",
  "has_torrent",
  "include_expunged",
  "disable_language_filter",
  "disable_uploader_filter",
  "disable_tag_filter",
] as const;

const LISTING_NAV_KEYS = ["seek", "jump"] as const;

function parseCsv(value: string | null): string[] {
  if (!value) return [];
  return value.split(",").filter(Boolean);
}

// Rebuilds listing filters from the URL. Inactive advanced options (zero page
// bounds, false flags) are dropped so they never reach the API request.
export function parseListFilters(
  searchParams: URLSearchParams,
): GalleryListFilters {
  const filters: GalleryListFilters = pickAdvancedOptions(
    parseAdvancedParams(searchParams),
  );
  const tags = parseCsv(searchParams.get("tags"));
  const categories = parseCsv(searchParams.get("categories"));
  if (tags.length > 0) filters.tags = tags;
  if (categories.length > 0) filters.categories = categories;
  return filters;
}

export function serializeListFilters(
  filters: GalleryListFilters,
): URLSearchParams {
  const params = serializeAdvancedParams(filters);
  if (filters.tags && filters.tags.length > 0) {
    params.set("tags", filters.tags.join(","));
  }
  if (filters.categories && filters.categories.length > 0) {
    params.set("categories", filters.categories.join(","));
  }
  return params;
}

export function parseNavOptions(
  searchParams: URLSearchParams,
): ListingNavOptions {
  return {
    seek: searchParams.get("seek") ?? undefined,
    jump: searchParams.get("jump") ?? undefined,
  };
}

export function serializeNavOptions(nav: ListingNavOptions): URLSearchParams {
  const params = new URLSearchParams();
  if (nav.seek) params.set("seek", nav.seek);
  if (nav.jump) params.set("jump", nav.jump);
  return params;
}

// Returns a copy of searchParams with the listing filters replaced, leaving any
// unrelated params intact.
export function withListFilters(
  searchParams: URLSearchParams,
  filters: GalleryListFilters,
): URLSearchParams {
  const params = new URLSearchParams(searchParams);
  for (const key of LISTING_FILTER_KEYS) params.delete(key);
  serializeListFilters(filters).forEach((value, key) => params.set(key, value));
  return params;
}

export function withNavOptions(
  searchParams: URLSearchParams,
  nav: ListingNavOptions,
): URLSearchParams {
  const params = new URLSearchParams(searchParams);
  for (const key of LISTING_NAV_KEYS) params.delete(key);
  serializeNavOptions(nav).forEach((value, key) => params.set(key, value));
  return params;
}
