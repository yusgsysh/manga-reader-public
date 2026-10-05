export type PrefillStatus =
  | "queued"
  | "running"
  | "completed"
  | "cancelled"
  | "failed";

export interface PrefillProgress {
  done: number;
  cached: number;
  fetched: number;
}

export interface PrefillItemError {
  index: number;
  url: string;
  error: string;
}

export interface PrefillJob {
  id: string;
  gallery_id: number | null;
  token: string;
  title: string;
  status: PrefillStatus;
  total: number;
  progress: PrefillProgress | null;
  failed_count: number;
  errors: PrefillItemError[];
  created_at: string;
  updated_at: string;
  finished_at: string | null;
}

export interface PrefillListResponse {
  jobs: PrefillJob[];
}

export interface PrefillStartRequest {
  gallery_id?: number;
  token?: string;
  title?: string;
  urls: string[];
}

export interface PrefillCleanupResponse {
  days: number;
  deleted: number;
}
