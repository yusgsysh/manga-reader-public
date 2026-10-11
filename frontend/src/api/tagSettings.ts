import { apiGet, apiPut } from "./client";

/** Like (blue) or dislike (red); tags without a preference keep default style. */
export type TagAction = "like" | "dislike";

export interface TagPreference {
  /** `namespace:name` (lowercased). */
  tag: string;
  action: TagAction;
}

export interface TagSettings {
  tags: TagPreference[];
}

export function fetchTagSettings(): Promise<TagSettings> {
  return apiGet<TagSettings>("/api/tag-settings");
}

/** Full-replace save; the server normalizes, dedupes and sorts. */
export function saveTagSettings(settings: TagSettings): Promise<TagSettings> {
  return apiPut<TagSettings>("/api/tag-settings", settings);
}
