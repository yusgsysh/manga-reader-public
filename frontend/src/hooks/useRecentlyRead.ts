import { useMemo } from "react";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { fetchRecentlyRead } from "../api/recentlyRead";
import { cleanupReadingProgress } from "../api/progress";
import { RECENTLY_READ_STALE_TIME } from "../lib/cacheConfig";
import { whenProgressSavesSettled } from "../lib/progressSave";

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

  // Metadata (title/thumbnail/pages/category) is persisted with the reading
  // record on the backend, so no per-item gallery lookups are needed here.
  const items = useMemo(
    () => listQuery.data?.pages.flatMap((page) => page.results) ?? [],
    [listQuery.data],
  );

  return { ...listQuery, items };
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
