import { apiGet } from "./client";
import type { SearchResponse } from "../types/gallery";

export function fetchSearch(params: {
  q: string;
  site?: string;
  categories?: string;
  page?: number;
}): Promise<SearchResponse> {
  return apiGet<SearchResponse>("/api/search", {
    q: params.q,
    site: params.site,
    categories: params.categories,
    page: params.page,
  });
}
