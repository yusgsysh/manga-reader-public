import { apiGet, apiPut } from "./client";

/** Returned in place of a stored token; sending it back keeps the secret. */
export const SYNC_TOKEN_MASK = "********";

export interface SyncConfig {
  enabled: boolean;
  server_url: string;
  /** Masked (********) when a token is stored, empty otherwise. */
  token: string;
}

export interface SyncConfigUpdate {
  enabled?: boolean;
  server_url?: string;
  /** Omit or send "" to clear; send the mask to keep the stored token. */
  token?: string;
}

export interface SyncStatus {
  enabled: boolean;
  configured: boolean;
  server_url: string;
  sse_connected: boolean;
  last_sync_at?: string;
  last_error?: string;
  cursor: number;
  last_pushed_id: number;
  pending: number;
}

export function fetchSyncConfig(): Promise<SyncConfig> {
  return apiGet<SyncConfig>("/api/sync/config");
}

export function saveSyncConfig(update: SyncConfigUpdate): Promise<SyncConfig> {
  return apiPut<SyncConfig>("/api/sync/config", update);
}

export function fetchSyncStatus(): Promise<SyncStatus> {
  return apiGet<SyncStatus>("/api/sync/status");
}
