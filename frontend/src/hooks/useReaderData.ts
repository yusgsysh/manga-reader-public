import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { fetchGallery, fetchGalleryPages } from "../api/gallery";
import { fetchReadingProgress, updateReadingProgress } from "../api/progress";
import { trackProgressSave } from "../lib/progressSave";
import type { GalleryCategory } from "../types/gallery";
import type { Gallery, ReadingProgress } from "../types/reader";
import type { RecentlyReadItem, RecentlyReadResponse } from "../types/recentlyRead";

const GALLERY_STALE_TIME = 5 * 60_000;
const PAGES_STALE_TIME = 10 * 60_000;
const PROGRESS_STALE_TIME = 30_000;
const RECENTLY_READ_LIMIT = 25;

type ProgressReading = NonNullable<RecentlyReadItem["reading"]>;

export function useGallery(id: number, token: string) {
  return useQuery({
    queryKey: ["gallery", id, token],
    queryFn: () => fetchGallery(id, token),
    enabled: Number.isFinite(id) && token.length > 0,
    staleTime: GALLERY_STALE_TIME,
  });
}

export function useGalleryPages(id: number, token: string) {
  return useQuery({
    queryKey: ["gallery-pages", id, token],
    queryFn: () => fetchGalleryPages(id, token),
    enabled: Number.isFinite(id) && token.length > 0,
    staleTime: PAGES_STALE_TIME,
  });
}

export function useReadingProgress(id: number, token: string) {
  return useQuery({
    queryKey: ["reading-progress", id, token],
    queryFn: () => fetchReadingProgress(id, token),
    enabled: Number.isFinite(id) && token.length > 0,
    staleTime: PROGRESS_STALE_TIME,
  });
}

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

export function updateRecentlyReadCache(
  queryClient: QueryClient,
  id: number,
  token: string,
  progress: ReadingProgress,
): void {
  const cached = queryClient.getQueryData<RecentlyReadResponse>(["recently-read"]);
  if (!cached) return;

  const reading = toReading(progress);
  const existing = cached.results.find(
    (item) => item.id === id && item.token === token,
  );
  const current: RecentlyReadItem = existing
    ? { ...existing, reading }
    : buildItem(queryClient, id, token, reading);

  const rest = cached.results
    .filter((item) => !(item.id === id && item.token === token))
    .sort((a, b) =>
      (b.reading?.updated_at ?? "").localeCompare(a.reading?.updated_at ?? ""),
    );

  queryClient.setQueryData<RecentlyReadResponse>(["recently-read"], {
    ...cached,
    results: [current, ...rest].slice(0, RECENTLY_READ_LIMIT),
  });
}

export function applyReadingProgressToCaches(
  queryClient: QueryClient,
  id: number,
  token: string,
  progress: ReadingProgress,
): void {
  void queryClient.cancelQueries({ queryKey: ["recently-read"] });
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
    mutationFn: (body: {
      current_page: number;
      progress: number;
      completed: boolean;
    }) => {
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
