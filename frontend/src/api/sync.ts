import { apiDelete, apiGet, apiPost, apiPostJson, apiPut } from "./client";

/** Returned in place of a stored node API key; sending it back keeps the secret. */
export const SYNC_KEY_MASK = "********";

export interface SyncConfig {
  enabled: boolean;
  server_url: string;
  /** Masked (********) when an API key is stored, empty otherwise. */
  token: string;
  /** Local account this configuration syncs (set when the config is saved). */
  username?: string;
}

export interface SyncConfigUpdate {
  enabled?: boolean;
  server_url?: string;
  /** Omit or send "" to clear; send the mask to keep the stored node API key. */
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
  /** This instance accepts peers, so it can mint user sync keys. */
  host_enabled: boolean;
}

export interface UserSyncKey {
  id: string;
  key_prefix: string;
  status: "active" | "revoked";
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
  revoked_at: string | null;
}

export interface CreatedUserSyncKey {
  /** Full plaintext key, returned only once. */
  key: string;
  api_key: UserSyncKey;
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

export interface SyncResyncResult {
  ok: boolean;
}

/**
 * Clears the replication cursor and wakes the engine, so the next exchange
 * sends this instance's whole dataset and pulls the peer's full snapshot.
 * Used by the settings page to repair two sides that drifted apart.
 */
export function startFullSync(): Promise<SyncResyncResult> {
  return apiPost<SyncResyncResult>("/api/sync/resync");
}

/** Keys bound to the current account, for peers that sync only this user. */
export function fetchUserSyncKeys(): Promise<{ keys: UserSyncKey[] }> {
  return apiGet<{ keys: UserSyncKey[] }>("/api/sync/keys");
}

export function createUserSyncKey(): Promise<CreatedUserSyncKey> {
  return apiPostJson<CreatedUserSyncKey>("/api/sync/keys");
}

export function revokeUserSyncKey(id: string): Promise<UserSyncKey> {
  return apiDelete<UserSyncKey>(`/api/sync/keys/${id}`);
}
