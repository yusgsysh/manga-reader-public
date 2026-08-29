import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchGallery, fetchGalleryPages } from "../api/gallery";
import { fetchReadingProgress, updateReadingProgress } from "../api/progress";

export function useGallery(id: number, token: string) {
  return useQuery({
    queryKey: ["gallery", id, token],
    queryFn: () => fetchGallery(id, token),
    enabled: Number.isFinite(id) && token.length > 0,
  });
}

export function useGalleryPages(id: number, token: string) {
  return useQuery({
    queryKey: ["gallery-pages", id, token],
    queryFn: () => fetchGalleryPages(id, token),
    enabled: Number.isFinite(id) && token.length > 0,
  });
}

export function useReadingProgress(id: number, token: string) {
  return useQuery({
    queryKey: ["reading-progress", id, token],
    queryFn: () => fetchReadingProgress(id, token),
    enabled: Number.isFinite(id) && token.length > 0,
  });
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
      queryClient.invalidateQueries({ queryKey: ["bookshelf"] });
      queryClient.invalidateQueries({ queryKey: ["recently-read"] });
    },
  });
}
