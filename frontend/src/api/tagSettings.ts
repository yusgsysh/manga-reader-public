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

/**
 * One side of a sync conflict: the preference on that side, or null when the
 * tag was deleted there (locally removed / gone from source My Tags).
 */
export type TagSyncSide = TagPreference | null;

/** Tag changed on both sides since the last sync; the user picks one. */
export interface TagSyncConflict {
  tag: string;
  local: TagSyncSide;
  remote: TagSyncSide;
}

/** Result of POST /api/tag-settings/sync (three-way merge with source My Tags). */
export interface TagSyncResult {
  added: number;
  updated: number;
  deleted: number;
  /** Rules imported/updated/removed from the source (never writes the source). */
  pulled: number;
  unchanged: number;
  /** Local-only deletions not yet applied to the source (need confirmation). */
  pending_deletes: string[];
  /** Both sides changed; unresolved tags were left untouched on both sides. */
  conflicts: TagSyncConflict[];
  failed: TagSyncFailure[];
  /** Full merged rule list (sorted by tag) — replaces the local cache. */
  tags: TagPreference[];
}

export function fetchTagSettings(): Promise<TagSettings> {
  return apiGet<TagSettings>("/api/tag-settings");
}

/** Full-replace save; the server normalizes, defaults/validates weight, dedupes and sorts. */
export function saveTagSettings(settings: TagSettings): Promise<TagSettings> {
  return apiPut<TagSettings>("/api/tag-settings", settings);
}

export interface SyncTagSettingsOptions {
  /** Apply pending source deletions of locally-deleted tags. */
  confirmDeletes?: boolean;
  /**
   * Resolve every conflict in this sync at once: "remote" keeps the source
   * value (pull), "local" keeps the local value (push). Omitted = only report.
   */
  conflictResolution?: "remote" | "local";
}

/**
 * Bidirectional sync: pulls source changes into the local rules, pushes local
 * changes to source My Tags, and reports tags changed on both sides.
 */
export function syncTagSettings(
  options: SyncTagSettingsOptions = {},
): Promise<TagSyncResult> {
  return apiPostJson<TagSyncResult>("/api/tag-settings/sync", {
    confirm_deletes: options.confirmDeletes ?? false,
    ...(options.conflictResolution
      ? { conflict_resolution: options.conflictResolution }
      : {}),
  });
}
