import { apiGet, apiPost, apiDelete } from "./client";
import type { BookshelfListResponse } from "../types/gallery";

export function fetchBookshelf(page: number): Promise<BookshelfListResponse> {
  return apiGet<BookshelfListResponse>("/api/bookshelf", { page });
}

export interface BookshelfMutationResult {
  success: boolean;
  in_bookshelf: boolean;
  // True when the add was satisfied from cached metadata because the upstream
  // ExHentai API was unreachable.
  offline?: boolean;
}

export function addToBookshelf(
  id: number,
  token: string,
): Promise<BookshelfMutationResult> {
  return apiPost(`/api/bookshelf/${id}/${token}`);
}

export function removeFromBookshelf(
  id: number,
  token: string,
): Promise<BookshelfMutationResult> {
  return apiDelete(`/api/bookshelf/${id}/${token}`);
}

export function getBookshelfStatus(
  id: number,
  token: string,
): Promise<{ in_bookshelf: boolean; created_at?: string }> {
  return apiGet(`/api/bookshelf/${id}/${token}/status`);
}
