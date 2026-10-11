import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Tag } from "../types/gallery";
import {
  fetchTagSettings,
  saveTagSettings,
  syncTagSettings,
  type SyncTagSettingsOptions,
  type TagAction,
  type TagPreference,
  type TagSettings,
  type TagSyncConflict,
  type TagSyncResult,
} from "../api/tagSettings";

export const TAG_SETTINGS_QUERY_KEY = ["tag-settings"] as const;

/**
 * Solid-background chip classes matching the source My Tags colours
 * (#3377FF blue / #FF6666 red, #f1f1f1 text). Applied through kumo's
 * tailwind-merge `cn`, so they override base chip backgrounds.
 */
export const TAG_ACTION_CHIP_CLASS: Record<TagAction, string> = {
  like: "bg-[#3377FF] text-[#f1f1f1]",
  dislike: "bg-[#FF6666] text-[#f1f1f1]",
};

export function tagKey(namespace: string, name: string): string {
  return namespace ? `${namespace}:${name}` : name;
}

/** Display order rank: liked (blue) first, disliked (red) second, neutral last. */
const ACTION_ORDER: Record<TagAction, number> = { like: 0, dislike: 1 };
const NEUTRAL_ORDER = 2;

/** Full preference for one tag: action + sort weight (mirrors My Tags). */
export interface TagRule {
  action: TagAction;
  weight: number;
}

/**
 * Stable sort placing preferred tags ahead of neutral ones (blue before red
 * before default), strongest |weight| first inside each group — the source
 * site orders flagged tags the same way. Equal rank+weight tags keep their
 * original relative order.
 */
export function sortTagsByPreference<T>(
  tags: T[],
  getRule: (tag: T) => TagRule | null,
): T[] {
  return [...tags].sort((a, b) => {
    const ruleA = getRule(a);
    const ruleB = getRule(b);
    const rankA = ruleA ? ACTION_ORDER[ruleA.action] : NEUTRAL_ORDER;
    const rankB = ruleB ? ACTION_ORDER[ruleB.action] : NEUTRAL_ORDER;
    if (rankA !== rankB) return rankA - rankB;
    const weightA = ruleA ? Math.abs(ruleA.weight) : 0;
    const weightB = ruleB ? Math.abs(ruleB.weight) : 0;
    return weightB - weightA;
  });
}

/**
 * Loads the persisted like/dislike tag preferences and exposes a fast lookup.
 * The query is shared through the global QueryClient cache, so every consumer
 * (detail page, card popovers, search chips) re-uses one request.
 */
export function useTagPreferences() {
  const query = useQuery({
    queryKey: TAG_SETTINGS_QUERY_KEY,
    queryFn: fetchTagSettings,
    staleTime: 5 * 60_000,
  });

  const ruleByKey = useMemo(() => {
    const map = new Map<string, TagRule>();
    for (const pref of query.data?.tags ?? []) {
      map.set(pref.tag, { action: pref.action, weight: pref.weight });
    }
    return map;
  }, [query.data]);

  const getTagRule = useMemo(() => {
    return (tag: Tag | string): TagRule | null => {
      if (typeof tag === "string") {
        return ruleByKey.get(tag.toLowerCase()) ?? null;
      }
      return ruleByKey.get(tagKey(tag.namespace, tag.name)) ?? null;
    };
  }, [ruleByKey]);

  return {
    /** Raw list as returned by the API (ordered by tag). */
    preferences: query.data?.tags ?? [],
    isLoading: query.isLoading,
    isError: query.isError,
    getTagRule,
  };
}

/** Mutation used by the settings page to full-replace the preference list. */
export function useSaveTagPreferences() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (settings: TagSettings) => saveTagSettings(settings),
    onSuccess: (data) => {
      queryClient.setQueryData(TAG_SETTINGS_QUERY_KEY, data);
    },
  });
}

/** Mutation running the bidirectional sync against source My Tags. */
export function useSyncTagPreferences() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (options: SyncTagSettingsOptions) => syncTagSettings(options),
    onSuccess: (result) => {
      // Pulled changes mean the server-side rules changed underneath us;
      // the response carries the merged list, so reseed the shared cache.
      if (result.pulled > 0) {
        queryClient.setQueryData(TAG_SETTINGS_QUERY_KEY, { tags: result.tags });
      }
    },
  });
}

export type {
  SyncTagSettingsOptions,
  TagAction,
  TagPreference,
  TagSettings,
  TagSyncConflict,
  TagSyncResult,
};
