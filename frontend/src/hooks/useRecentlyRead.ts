import { useMemo } from "react";
import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQueryClient,
} from "@tanstack/react-query";
import { fetchRecentlyRead } from "../api/recentlyRead";
import { fetchGallery } from "../api/gallery";
import { cleanupReadingProgress } from "../api/progress";
import { whenProgressSavesSettled } from "../lib/progressSave";
import type { Gallery } from "../types/reader";
import type { RecentlyReadItem } from "../types/recentlyRead";

function needsEnrichment(item: RecentlyReadItem): boolean {
  return !item.title || !item.thumbnail || !item.pages;
}

const RECENTLY_READ_STALE_TIME = 2 * 60_000;

export function useRecentlyRead() {
  const listQuery = useInfiniteQuery({
    queryKey: ["recently-read"],
    queryFn: async ({ pageParam }) => {
      await whenProgressSavesSettled();
      return fetchRecentlyRead(pageParam);
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage) =>
      lastPage.page + 1 < lastPage.total_pages
        ? lastPage.page + 1
        : undefined,
    staleTime: RECENTLY_READ_STALE_TIME,
  });

  const items = useMemo(
    () => listQuery.data?.pages.flatMap((page) => page.results) ?? [],
    [listQuery.data],
  );
  const missing = useMemo(() => items.filter(needsEnrichment), [items]);

  // Galleries removed from the bookshelf lack title/thumbnail in the
  // recently-read response. Fetch their metadata and merge it back.
  const galleryQueries = useQueries({
    queries: missing.map((item) => ({
      queryKey: ["gallery", item.id, item.token],
      queryFn: () => fetchGallery(item.id, item.token),
      enabled: listQuery.isSuccess,
    })),
  });

  const lookup = useMemo(() => {
    const map = new Map<number, Gallery>();
    missing.forEach((item, index) => {
      const gallery = galleryQueries[index]?.data;
      if (gallery) map.set(item.id, gallery);
    });
    return map;
  }, [missing, galleryQueries]);

  const enrichItem = useMemo(
    () => (item: RecentlyReadItem): RecentlyReadItem => {
      const gallery = lookup.get(item.id);
      if (!gallery) return item;
      return {
        ...item,
        title: item.title || gallery.title,
        title_jpn: item.title_jpn || gallery.title_jpn,
        thumbnail: item.thumbnail || gallery.thumbnail,
        pages: item.pages || gallery.page_count,
        category: (item.category ||
          gallery.category) as RecentlyReadItem["category"],
      };
    },
    [lookup],
  );

  const data = useMemo(() => {
    if (!listQuery.data) return listQuery.data;
    return {
      ...listQuery.data,
      pages: listQuery.data.pages.map((page) => ({
        ...page,
        results: page.results.map(enrichItem),
      })),
    };
  }, [listQuery.data, enrichItem]);

  const enrichedItems = useMemo(
    () => data?.pages.flatMap((page) => page.results) ?? [],
    [data],
  );

  return { ...listQuery, data, items: enrichedItems };
}

export function useCleanupReadingProgress() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (days: number) => cleanupReadingProgress(days),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["recently-read"] });
      queryClient.invalidateQueries({ queryKey: ["bookshelf"] });
    },
  });
}
