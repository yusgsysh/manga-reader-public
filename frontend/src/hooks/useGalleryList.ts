import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchGalleries, fetchWatched, fetchPopular } from "../api/gallery";
import type {
  AdvancedSearchOptions,
  GalleryListResponse,
} from "../types/gallery";

const GALLERY_LIST_STALE_TIME = 2 * 60_000;

// ExHentai serves 25 galleries per listing page.
const EXHENTAI_PAGE_SIZE = 25;

function nextPageParam(lastPage: GalleryListResponse): number | undefined {
  return lastPage.results.length >= EXHENTAI_PAGE_SIZE
    ? lastPage.page + 1
    : undefined;
}

export function useGalleries(filters?: AdvancedSearchOptions) {
  return useInfiniteQuery({
    queryKey: ["home", filters],
    queryFn: ({ pageParam }) => fetchGalleries(pageParam, filters),
    initialPageParam: 0,
    getNextPageParam: nextPageParam,
    staleTime: GALLERY_LIST_STALE_TIME,
  });
}

export function useWatched(filters?: AdvancedSearchOptions) {
  return useInfiniteQuery({
    queryKey: ["watched", filters],
    queryFn: ({ pageParam }) => fetchWatched(pageParam, filters),
    initialPageParam: 0,
    getNextPageParam: nextPageParam,
    staleTime: GALLERY_LIST_STALE_TIME,
  });
}

export function usePopular() {
  return useInfiniteQuery({
    queryKey: ["popular"],
    queryFn: ({ pageParam }) => fetchPopular(pageParam),
    initialPageParam: 0,
    getNextPageParam: nextPageParam,
    staleTime: GALLERY_LIST_STALE_TIME,
  });
}
