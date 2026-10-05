import {
  apiDelete,
  apiGet,
  apiHead,
  apiPost,
  apiPostJson,
  ApiRequestError,
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

export function cancelPrefillJob(id: string): Promise<PrefillJob> {
  return apiPost<PrefillJob>(`/api/prefill/${id}/cancel`);
}

export function deletePrefillJob(id: string): Promise<{ deleted: number }> {
  return apiDelete<{ deleted: number }>(`/api/prefill/${id}`);
}

export function cleanupPrefillJobs(
  days: number,
): Promise<PrefillCleanupResponse> {
  return apiPost<PrefillCleanupResponse>("/api/prefill/cleanup", { days });
}

export async function headPrefillZip(id: string): Promise<boolean> {
  try {
    await apiHead(`/api/prefill/${id}/zip`);
    return true;
  } catch (err) {
    if (err instanceof ApiRequestError) {
      return false;
    }
    throw err;
  }
}

export function prefillZipUrl(id: string): string {
  return buildApiUrl(`/api/prefill/${id}/zip`);
}
