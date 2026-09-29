import { useQuery } from "@tanstack/react-query";
import { fetchGalleries, fetchWatched, fetchPopular } from "../api/gallery";
import type { AdvancedSearchOptions } from "../types/gallery";

const GALLERY_LIST_STALE_TIME = 2 * 60_000;

export function useGalleries(page: number, filters?: AdvancedSearchOptions) {
  return useQuery({
    queryKey: ["home", page, filters],
    queryFn: () => fetchGalleries(page, filters),
    staleTime: GALLERY_LIST_STALE_TIME,
  });
}

export function useWatched(page: number, filters?: AdvancedSearchOptions) {
  return useQuery({
    queryKey: ["watched", page, filters],
    queryFn: () => fetchWatched(page, filters),
    staleTime: GALLERY_LIST_STALE_TIME,
  });
}

export function usePopular(page: number) {
  return useQuery({
    queryKey: ["popular", page],
    queryFn: () => fetchPopular(page),
    staleTime: GALLERY_LIST_STALE_TIME,
  });
}
