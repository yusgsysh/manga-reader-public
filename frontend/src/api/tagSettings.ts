import { apiGet, apiPostJson, apiPut } from "./client";

/** Like (blue) or dislike (red); tags without a preference keep default style. */
export type TagAction = "like" | "dislike";

export interface TagPreference {
  /** `namespace:name` (lowercased). */
  tag: string;
  action: TagAction;
  /**
   * Sort weight mirroring ExHentai My Tags: like >= 0, dislike <= 0.
   * Orders tags (strongest first); never filters inside this app.
   */
  weight: number;
}

export interface TagSettings {
  tags: TagPreference[];
}

/** One tag the sync could not apply on the source. */
export interface TagSyncFailure {
  tag: string;
  error: string;
}

/** Result of POST /api/tag-settings/sync (full mirror to source My Tags). */
export interface TagSyncResult {
  added: number;
  updated: number;
  deleted: number;
  unchanged: number;
  /** Remote-only tags not yet deleted (need confirm_deletes=true). */
  pending_deletes: string[];
  failed: TagSyncFailure[];
}

export function fetchTagSettings(): Promise<TagSettings> {
  return apiGet<TagSettings>("/api/tag-settings");
}

/** Full-replace save; the server normalizes, defaults/validates weight, dedupes and sorts. */
export function saveTagSettings(settings: TagSettings): Promise<TagSettings> {
  return apiPut<TagSettings>("/api/tag-settings", settings);
}

/**
 * Mirror the local rules onto the source My Tags page. Adds/updates always
 * run; remote-only rows are deleted only when confirmDeletes is true.
 */
export function syncTagSettings(confirmDeletes = false): Promise<TagSyncResult> {
  return apiPostJson<TagSyncResult>("/api/tag-settings/sync", {
    confirm_deletes: confirmDeletes,
  });
}
