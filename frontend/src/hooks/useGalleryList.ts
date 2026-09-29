import { useQuery } from "@tanstack/react-query";
import { fetchGalleries, fetchWatched, fetchPopular } from "../api/gallery";
import type { AdvancedSearchOptions } from "../types/gallery";

export function useGalleries(page: number, filters?: AdvancedSearchOptions) {
  return useQuery({
    queryKey: ["home", page, filters],
    queryFn: () => fetchGalleries(page, filters),
  });
}

export function useWatched(page: number, filters?: AdvancedSearchOptions) {
  return useQuery({
    queryKey: ["watched", page, filters],
    queryFn: () => fetchWatched(page, filters),
  });
}

export function usePopular(page: number) {
  return useQuery({
    queryKey: ["popular", page],
    queryFn: () => fetchPopular(page),
  });
}
