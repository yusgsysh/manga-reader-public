import { apiGet } from "./client";
import type { GalleryDetail, GalleryListResponse } from "../types/gallery";

export function fetchGallerys(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/gallerys", { page });
}

export function fetchWatched(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/watched", { page });
}

export function fetchPopular(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/popular", { page });
}

export function fetchGalleryDetail(
  id: number,
  token: string,
): Promise<GalleryDetail> {
  return apiGet<GalleryDetail>(`/api/gallery/${id}/${token}/details`);
}
