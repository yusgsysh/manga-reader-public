import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchGalleries, fetchWatched, fetchPopular } from "../api/gallery";
import { GALLERY_LIST_STALE_TIME } from "../lib/cacheConfig";
import type {
  AdvancedSearchOptions,
  GalleryListResponse,
  ListingNavOptions,
} from "../types/gallery";

// ExHentai serves 25 galleries per listing page.
const EXHENTAI_PAGE_SIZE = 25;

function nextPageParam(lastPage: GalleryListResponse): number | undefined {
  return lastPage.results.length >= EXHENTAI_PAGE_SIZE
    ? lastPage.page + 1
    : undefined;
}

export function useGalleries(
  filters?: AdvancedSearchOptions,
  nav?: ListingNavOptions,
) {
  return useInfiniteQuery({
    queryKey: ["home", filters, nav],
    queryFn: ({ pageParam }) => fetchGalleries(pageParam, filters, nav),
    initialPageParam: 0,
    getNextPageParam: nextPageParam,
    staleTime: GALLERY_LIST_STALE_TIME,
  });
}

export function useWatched(
  filters?: AdvancedSearchOptions,
  nav?: ListingNavOptions,
) {
  return useInfiniteQuery({
    queryKey: ["watched", filters, nav],
    queryFn: ({ pageParam }) => fetchWatched(pageParam, filters, nav),
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
