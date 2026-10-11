import { useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Tag } from "../types/gallery";
import {
  fetchTagSettings,
  saveTagSettings,
  type TagAction,
  type TagPreference,
  type TagSettings,
} from "../api/tagSettings";

export const TAG_SETTINGS_QUERY_KEY = ["tag-settings"] as const;

/** Tailwind classes colouring a tag's text by preference (light / dark). */
export const TAG_ACTION_TEXT_CLASS: Record<TagAction, string> = {
  like: "text-blue-600 dark:text-blue-400",
  dislike: "text-red-600 dark:text-red-400",
};

export function tagKey(namespace: string, name: string): string {
  return namespace ? `${namespace}:${name}` : name;
}

/** Display order: liked (blue) first, disliked (red) second, neutral last. */
const ACTION_ORDER: Record<TagAction, number> = { like: 0, dislike: 1 };
const NEUTRAL_ORDER = 2;

/**
 * Stable sort placing preferred tags ahead of neutral ones (blue before red
 * before default), matching the source site's tag-list ordering. Equal-rank
 * tags keep their original relative order.
 */
export function sortTagsByPreference<T>(
  tags: T[],
  getAction: (tag: T) => TagAction | null,
): T[] {
  return [...tags].sort((a, b) => {
    const actionA = getAction(a);
    const actionB = getAction(b);
    const rankA = actionA ? ACTION_ORDER[actionA] : NEUTRAL_ORDER;
    const rankB = actionB ? ACTION_ORDER[actionB] : NEUTRAL_ORDER;
    return rankA - rankB;
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

  const actionByKey = useMemo(() => {
    const map = new Map<string, TagAction>();
    for (const pref of query.data?.tags ?? []) {
      map.set(pref.tag, pref.action);
    }
    return map;
  }, [query.data]);

  const getTagAction = useMemo(() => {
    return (tag: Tag | string): TagAction | null => {
      if (typeof tag === "string") {
        return actionByKey.get(tag.toLowerCase()) ?? null;
      }
      return actionByKey.get(tagKey(tag.namespace, tag.name)) ?? null;
    };
  }, [actionByKey]);

  return {
    /** Raw list as returned by the API (ordered by tag). */
    preferences: query.data?.tags ?? [],
    isLoading: query.isLoading,
    isError: query.isError,
    getTagAction,
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

export type { TagAction, TagPreference, TagSettings };
