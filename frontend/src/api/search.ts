import { apiGet } from "./client";
import type { SearchParams, SearchResponse } from "../types/gallery";

function boolToStr(v?: boolean): string | undefined {
  return v === undefined ? undefined : v ? "true" : "false";
}

export function fetchSearch(params: SearchParams): Promise<SearchResponse> {
  return apiGet<SearchResponse>("/api/search", {
    q: params.q,
    site: params.site,
    categories: params.categories,
    page: params.page,
    min_pages: params.min_pages,
    max_pages: params.max_pages,
    min_rating: params.min_rating,
    has_torrent: boolToStr(params.has_torrent),
    include_expunged: boolToStr(params.include_expunged),
    search_name: boolToStr(params.search_name),
    search_tags: boolToStr(params.search_tags),
    search_description: boolToStr(params.search_description),
    include_low_power_tags: boolToStr(params.include_low_power_tags),
    include_downvoted_tags: boolToStr(params.include_downvoted_tags),
    disable_language_filter: boolToStr(params.disable_language_filter),
    disable_uploader_filter: boolToStr(params.disable_uploader_filter),
    disable_tag_filter: boolToStr(params.disable_tag_filter),
  });
}
