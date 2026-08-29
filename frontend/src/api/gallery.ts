import { apiGet } from "./client";
import type {
  GalleryDetail,
  GalleryListResponse,
} from "../types/gallery";
import type { Gallery, GalleryPagesResponse } from "../types/reader";

export function fetchGallerys(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/gallerys", { page });
}

export function fetchWatched(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/watched", { page });
}

export function fetchPopular(page: number): Promise<GalleryListResponse> {
  return apiGet<GalleryListResponse>("/api/popular", { page });
}

export function fetchGallery(id: number, token: string): Promise<Gallery> {
  return apiGet<Gallery>(`/api/gallery/${id}/${token}`);
}

export function fetchGalleryPages(
  id: number,
  token: string,
): Promise<GalleryPagesResponse> {
  return apiGet<GalleryPagesResponse>(`/api/gallery/${id}/${token}/pages`);
}

export function fetchGalleryDetail(
  id: number,
  token: string,
): Promise<GalleryDetail> {
  return apiGet<GalleryDetail>(`/api/gallery/${id}/${token}/details`);
}
