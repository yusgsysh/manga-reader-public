import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchSearch } from "../api/search";
import type { SearchParams, SearchResponse } from "../types/gallery";

const SEARCH_STALE_TIME = 5 * 60_000;

export function useSearch(params: SearchParams) {
  return useInfiniteQuery({
    queryKey: [
      "search",
      params.q,
      params.site,
      params.categories,
      params.seek,
      params.jump,
      params.min_pages,
      params.max_pages,
      params.min_rating,
      params.has_torrent,
      params.include_expunged,
      params.search_name,
      params.search_tags,
      params.search_description,
      params.include_low_power_tags,
      params.include_downvoted_tags,
      params.disable_language_filter,
      params.disable_uploader_filter,
      params.disable_tag_filter,
    ],
    queryFn: ({ pageParam }) => fetchSearch({ ...params, page: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (lastPage: SearchResponse) =>
      lastPage.page + 1 < lastPage.total_pages
        ? lastPage.page + 1
        : undefined,
    enabled: (params.q ?? "").trim().length > 0,
    staleTime: SEARCH_STALE_TIME,
  });
}
