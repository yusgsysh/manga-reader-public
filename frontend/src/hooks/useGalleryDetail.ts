import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchGalleryDetail, fetchGalleryDetailCached } from "../api/gallery";
import {
  addToBookshelf,
  getBookshelfStatus,
  removeFromBookshelf,
} from "../api/bookshelf";
import type { GalleryDetail } from "../types/gallery";
import { useOnlineThenCached } from "./useOnlineCached";

export function useGalleryDetail(id: number, token: string) {
  return useOnlineThenCached<GalleryDetail>({
    enabled: Number.isFinite(id) && token.length > 0,
    onlineKey: ["gallery-detail", id, token],
    cacheKey: ["gallery-detail-cache", id, token],
    onlineFn: () => fetchGalleryDetail(id, token),
    cacheFn: () => fetchGalleryDetailCached(id, token),
    staleTime: 5 * 60_000,
  });
}

export function useBookshelfStatus(id: number, token: string) {
  return useQuery({
    queryKey: ["bookshelf-status", id, token],
    queryFn: () => getBookshelfStatus(id, token),
    enabled: Number.isFinite(id) && token.length > 0,
  });
}

export function useBookshelfToggle(id: number, token: string) {
  const queryClient = useQueryClient();

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["bookshelf-status", id, token] });
    queryClient.invalidateQueries({ queryKey: ["bookshelf"] });
  };

  const add = useMutation({
    mutationFn: () => addToBookshelf(id, token),
    onSuccess: invalidate,
  });

  const remove = useMutation({
    mutationFn: () => removeFromBookshelf(id, token),
    onSuccess: invalidate,
  });

  return { add, remove };
}
