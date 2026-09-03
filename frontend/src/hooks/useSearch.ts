import { useQuery } from "@tanstack/react-query";
import { fetchSearch } from "../api/search";
import type { SearchParams } from "../types/gallery";

export function useSearch(params: SearchParams) {
  return useQuery({
    queryKey: [
      "search",
      params.q,
      params.site,
      params.categories,
      params.page,
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
    queryFn: () => fetchSearch(params),
    enabled: (params.q ?? "").trim().length > 0,
  });
}
