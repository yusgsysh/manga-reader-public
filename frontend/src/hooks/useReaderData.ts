import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchGallery, fetchGalleryPages } from "../api/gallery";
import { fetchReadingProgress, updateReadingProgress } from "../api/progress";
import type { ReadingProgress } from "../types/reader";
import type { RecentlyReadResponse } from "../types/recentlyRead";

const GALLERY_STALE_TIME = 5 * 60_000;
const PAGES_STALE_TIME = 10 * 60_000;
const PROGRESS_STALE_TIME = 30_000;

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

function updateRecentlyReadCache(
  queryClient: ReturnType<typeof useQueryClient>,
  id: number,
  token: string,
  progress: ReadingProgress,
) {
  const old = queryClient.getQueryData<RecentlyReadResponse>(["recently-read"]);
  if (!old) return;

  const updated = old.results.map((item) => {
    if (item.id === id && item.token === token) {
      return {
        ...item,
        reading: {
          gallery_id: progress.gallery_id,
          token: progress.token,
          current_page: progress.current_page,
          progress: progress.progress,
          completed: progress.completed,
          started_at: progress.started_at ?? undefined,
          updated_at: progress.updated_at ?? undefined,
        },
      };
    }
    return item;
  });

  const sorted = [...updated].sort((a, b) => {
    const aTime = a.reading?.updated_at ?? "";
    const bTime = b.reading?.updated_at ?? "";
    return bTime.localeCompare(aTime);
  });

  queryClient.setQueryData(["recently-read"], { ...old, results: sorted });
}

export function useUpdateReadingProgress(id: number, token: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      current_page: number;
      progress: number;
      completed: boolean;
    }) => updateReadingProgress(id, token, body),
    onSuccess: (data) => {
      queryClient.setQueryData(["reading-progress", id, token], data);
      updateRecentlyReadCache(queryClient, id, token, data);
      queryClient.invalidateQueries({ queryKey: ["bookshelf"] });
      queryClient.invalidateQueries({ queryKey: ["recently-read"] });
    },
  });
}
