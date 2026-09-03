import { useCallback, useEffect, useSyncExternalStore } from "react";
import type { Tag } from "../types/gallery";
import { tagTranslationService } from "../services/tagTranslation";
import type { TagTranslationDatabaseInfo } from "../services/tagTranslation";

export function useTagTranslation() {
  const status = useSyncExternalStore(
    tagTranslationService.subscribe,
    tagTranslationService.getStatus,
  );
  const updateStatus = useSyncExternalStore(
    tagTranslationService.subscribe,
    tagTranslationService.getUpdateStatus,
  );
  const info = useSyncExternalStore(
    tagTranslationService.subscribe,
    tagTranslationService.getInfo,
  );

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
    searchTags,
    update,
  };
}
