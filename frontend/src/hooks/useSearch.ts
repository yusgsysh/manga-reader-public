import { useQuery } from "@tanstack/react-query";
import { fetchSearch } from "../api/search";

export function useSearch(params: {
  q: string;
  site?: string;
  categories?: string;
  page: number;
}) {
  return useQuery({
    queryKey: ["search", params.q, params.site, params.categories, params.page],
    queryFn: () => fetchSearch(params),
    enabled: params.q.trim().length > 0,
  });
}
