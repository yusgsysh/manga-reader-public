import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { fetchGallery, fetchGalleryPages } from "../api/gallery";
import { fetchReadingProgress, updateReadingProgress } from "../api/progress";
import { trackProgressSave } from "../lib/progressSave";
import type { ReadingProgress } from "../types/reader";

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
