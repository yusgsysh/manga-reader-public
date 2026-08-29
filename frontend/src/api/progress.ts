import { apiGet, apiPut } from "./client";
import type {
  ReadingProgress,
  UpdateReadingProgressRequest,
} from "../types/reader";

export function fetchReadingProgress(
  id: number,
  token: string,
): Promise<ReadingProgress> {
  return apiGet<ReadingProgress>(`/api/progress/${id}/${token}`);
}

export function updateReadingProgress(
  id: number,
  token: string,
  body: UpdateReadingProgressRequest,
): Promise<ReadingProgress> {
  return apiPut<ReadingProgress>(`/api/progress/${id}/${token}`, body);
}
