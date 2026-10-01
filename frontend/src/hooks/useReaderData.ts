import { useCallback, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import {
  fetchGallery,
  fetchGalleryCached,
  fetchGalleryPages,
  fetchGalleryPagesCached,
} from "../api/gallery";
import { fetchReadingProgress, updateReadingProgress } from "../api/progress";
import { trackProgressSave } from "../lib/progressSave";
import type { GalleryCategory } from "../types/gallery";
import type { RecentlyReadItem, RecentlyReadResponse } from "../types/recentlyRead";
import type {
  Gallery,
  GalleryPagesResponse,
  ReadingProgress,
  UpdateReadingProgressRequest,
} from "../types/reader";
import { useOnlineStatus, useOnlineThenCached } from "./useOnlineCached";

const GALLERY_STALE_TIME = 5 * 60_000;
const PAGES_STALE_TIME = 10 * 60_000;
const PROGRESS_STALE_TIME = 30_000;

export function useGallery(id: number, token: string) {
  return useOnlineThenCached<Gallery>({
    enabled: Number.isFinite(id) && token.length > 0,
    onlineKey: ["gallery", id, token],
    cacheKey: ["gallery-cache", id, token],
    onlineFn: () => fetchGallery(id, token),
    cacheFn: () => fetchGalleryCached(id, token),
    staleTime: GALLERY_STALE_TIME,
  });
}

export function useGalleryPages(id: number, token: string) {
  const enabled = Number.isFinite(id) && token.length > 0;
  const online = useOnlineStatus();
  const [partial, setPartial] = useState<GalleryPagesResponse | null>(null);

  // The page list is only ever cached when complete, so a cache hit is served
  // immediately and no live stream is started. Otherwise the live NDJSON
  // stream is read (progressively) and the backend caches it when complete.
  const cacheQuery = useQuery({
    queryKey: ["gallery-pages-cache", id, token],
    queryFn: () => fetchGalleryPagesCached(id, token),
    enabled,
    staleTime: PAGES_STALE_TIME,
    retry: 0,
  });

  const cacheSettled = cacheQuery.data !== undefined || cacheQuery.isError;

  const onlineFn = useCallback(
    ({ signal }: { signal: AbortSignal }) => {
      setPartial(null);
      return fetchGalleryPages(
        id,
        token,
        (snapshot) => setPartial(snapshot),
        signal,
      );
    },
    [id, token],
  );

  const onlineQuery = useQuery({
    queryKey: ["gallery-pages", id, token],
    queryFn: (context) => onlineFn(context),
    // Cache-first: only hit the live endpoint when no complete list is cached.
    enabled: enabled && online && cacheSettled && cacheQuery.data === undefined,
    staleTime: PAGES_STALE_TIME,
    retry: 0,
  });

  // Expose the snapshot only while the live stream is still running: once the
  // query fails or settles the partial list must disappear, so a broken stream
  // errors (or a later cache fallback wins) instead of leaking partial pages.
  // onlineFn resets the snapshot whenever a new stream starts.
  const streaming =
    partial !== null &&
    cacheQuery.data === undefined &&
    onlineQuery.data === undefined &&
    onlineQuery.isFetching;
  if (streaming) {
    return {
      data: partial,
      source: "online" as const,
      isLoading: false,
      isFetching: true,
      isError: false,
      error: null,
      refetch: () => {
        void onlineQuery.refetch();
      },
    };
  }

  const data = cacheQuery.data ?? onlineQuery.data;
  const source: "online" | "cache" | null =
    cacheQuery.data !== undefined
      ? "cache"
      : onlineQuery.data !== undefined
        ? "online"
        : null;
  const onlineErrored = !online || onlineQuery.isError;
  const isError = enabled && data === undefined && cacheSettled && onlineErrored;
  const isLoading = enabled && data === undefined && !isError;

  return {
    data,
    source,
    isLoading,
    isFetching: onlineQuery.isFetching,
    isError,
    error: isError
      ? ((cacheQuery.error ?? onlineQuery.error) as Error | null)
      : null,
    refetch: () => {
      if (online) void onlineQuery.refetch();
      void cacheQuery.refetch();
    },
  };
}

export function useReadingProgress(id: number, token: string) {
  return useQuery({
    queryKey: ["reading-progress", id, token],
    queryFn: () => fetchReadingProgress(id, token),
    enabled: Number.isFinite(id) && token.length > 0,
    staleTime: PROGRESS_STALE_TIME,
  });
}

type ProgressReading = NonNullable<RecentlyReadItem["reading"]>;

function toReading(progress: ReadingProgress): ProgressReading {
  return {
    gallery_id: progress.gallery_id,
    token: progress.token,
    current_page: progress.current_page,
    progress: progress.progress,
    completed: progress.completed,
    started_at: progress.started_at ?? undefined,
    updated_at: progress.updated_at ?? undefined,
  };
}

function buildItem(
  queryClient: QueryClient,
  id: number,
  token: string,
  reading: ProgressReading,
): RecentlyReadItem {
  const gallery = queryClient.getQueryData<Gallery>(["gallery", id, token]);
  return {
    id,
    token,
    title: gallery?.title ?? "",
    title_jpn: gallery?.title_jpn ?? "",
    category: (gallery?.category ?? "") as GalleryCategory,
    thumbnail: gallery?.thumbnail ?? "",
    pages: gallery?.page_count ?? 0,
    reading,
  };
}

// updateRecentlyReadCache moves the just-read gallery to the front of the
// recently-read infinite query so the correct order renders immediately when
// navigating back, before the background refetch confirms it. Enrichment
// metadata (title/thumbnail/pages/category) is preserved for existing items.
export function updateRecentlyReadCache(
  queryClient: QueryClient,
  id: number,
  token: string,
  progress: ReadingProgress,
): void {
  queryClient.setQueryData<InfiniteData<RecentlyReadResponse>>(
    ["recently-read"],
    (old) => {
      if (!old || old.pages.length === 0) return old;

      const reading = toReading(progress);
      let existing: RecentlyReadItem | undefined;
      const pages = old.pages.map((page) => ({
        ...page,
        results: page.results.filter((item) => {
          if (item.id === id && item.token === token) {
            existing = item;
            return false;
          }
          return true;
        }),
      }));

      const item: RecentlyReadItem = existing
        ? { ...existing, reading: { ...existing.reading, ...reading } }
        : buildItem(queryClient, id, token, reading);

      pages[0] = { ...pages[0], results: [item, ...pages[0].results] };
      return { ...old, pages };
    },
  );
}

export function applyReadingProgressToCaches(
  queryClient: QueryClient,
  id: number,
  token: string,
  progress: ReadingProgress,
): void {
  queryClient.setQueryData<ReadingProgress>(
    ["reading-progress", id, token],
    progress,
  );
  updateRecentlyReadCache(queryClient, id, token, progress);
}

export function invalidateReadingLists(queryClient: QueryClient): void {
  void queryClient.invalidateQueries({ queryKey: ["bookshelf"] });
  void queryClient.invalidateQueries({ queryKey: ["recently-read"] });
}

export function useUpdateReadingProgress(id: number, token: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: UpdateReadingProgressRequest) => {
      const save = updateReadingProgress(id, token, body);
      trackProgressSave(save);
      return save;
    },
    onSuccess: (data) => {
      applyReadingProgressToCaches(queryClient, id, token, data);
      invalidateReadingLists(queryClient);
    },
  });
}
