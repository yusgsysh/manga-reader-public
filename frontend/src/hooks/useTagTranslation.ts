import { useCallback, useEffect, useSyncExternalStore } from "react";
import type { Tag } from "../types/gallery";
import { tagTranslationService } from "../services/tagTranslation";
import type { TagTranslationDatabaseInfo } from "../services/tagTranslation";

export function useTagTranslation() {
  // One version subscription instead of three status/info subscriptions: the
  // snapshot is a plain counter, so React bails out unless the store changed.
  useSyncExternalStore(
    tagTranslationService.subscribe,
    tagTranslationService.getVersion,
    tagTranslationService.getVersion,
  );

  const status = tagTranslationService.getStatus();
  const updateStatus = tagTranslationService.getUpdateStatus();
  const info = tagTranslationService.getInfo();

  useEffect(() => {
    tagTranslationService.load();
  }, []);

  const translateTag = useCallback(
    (tag: Tag) => tagTranslationService.translateTag(tag),
    [],
  );

  const translateNamespace = useCallback(
    (namespace: string) => tagTranslationService.translateNamespace(namespace),
    [],
  );

  const hasTranslation = useCallback(
    (tag: Tag) => tagTranslationService.hasTranslation(tag),
    [],
  );

  const getTagInfo = useCallback(
    (tag: Tag) => tagTranslationService.lookup(tag),
    [],
  );

  const searchTags = useCallback(
    (query: string, limit?: number) => tagTranslationService.searchTags(query, limit),
    [],
  );

  const update = useCallback(
    () => tagTranslationService.update(),
    [],
  );

  return {
    status,
    updateStatus,
    info: info as TagTranslationDatabaseInfo | null,
    ready: status === "ready",
    translateTag,
    translateNamespace,
    hasTranslation,
    getTagInfo,
    searchTags,
    update,
  };
}
