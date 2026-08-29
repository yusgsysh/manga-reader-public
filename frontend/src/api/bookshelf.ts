import { apiGet, apiPost, apiDelete } from "./client";
import type { BookshelfListResponse } from "../types/gallery";

export function fetchBookshelf(page: number): Promise<BookshelfListResponse> {
  return apiGet<BookshelfListResponse>("/api/bookshelf", { page });
}

export function addToBookshelf(
  id: number,
  token: string,
): Promise<{ success: boolean; in_bookshelf: boolean }> {
  return apiPost(`/api/bookshelf/${id}/${token}`);
}

export function removeFromBookshelf(
  id: number,
  token: string,
): Promise<{ success: boolean; in_bookshelf: boolean }> {
  return apiDelete(`/api/bookshelf/${id}/${token}`);
}

export function getBookshelfStatus(
  id: number,
  token: string,
): Promise<{ in_bookshelf: boolean; added_at?: string }> {
  return apiGet(`/api/bookshelf/${id}/${token}/status`);
}
