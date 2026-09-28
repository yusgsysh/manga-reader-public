import {
  apiDelete,
  apiGet,
  apiPost,
  apiPostJson,
  buildApiUrl,
} from "./client";
import type {
  PrefillCleanupResponse,
  PrefillJob,
  PrefillListResponse,
  PrefillStartRequest,
} from "../types/prefill";

export function startPrefillJob(req: PrefillStartRequest): Promise<PrefillJob> {
  return apiPostJson<PrefillJob>("/api/prefill", req);
}

export function fetchPrefillJobs(): Promise<PrefillListResponse> {
  return apiGet<PrefillListResponse>("/api/prefill");
}

export function fetchPrefillJob(id: number): Promise<PrefillJob> {
  return apiGet<PrefillJob>(`/api/prefill/${id}`);
}

export function cancelPrefillJob(id: number): Promise<PrefillJob> {
  return apiPost<PrefillJob>(`/api/prefill/${id}/cancel`);
}

export function deletePrefillJob(id: number): Promise<{ deleted: number }> {
  return apiDelete<{ deleted: number }>(`/api/prefill/${id}`);
}

export function cleanupPrefillJobs(
  days: number,
): Promise<PrefillCleanupResponse> {
  return apiPost<PrefillCleanupResponse>("/api/prefill/cleanup", { days });
}

export function prefillZipUrl(id: number): string {
  return buildApiUrl(`/api/prefill/${id}/zip`);
}
