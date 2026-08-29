import { apiGet } from "./client";
import type { GalleryListResponse } from "../types/gallery";

export function fetchGallerys(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/gallerys", { page });
}

export function fetchWatched(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/watched", { page });
}

export function fetchPopular(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/popular", { page });
}
