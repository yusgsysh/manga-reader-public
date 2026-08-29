import { useCallback, useEffect, useSyncExternalStore } from "react";
import type { Tag } from "../types/gallery";
import { tagTranslationService } from "../services/tagTranslation";

export function useTagTranslation() {
  const status = useSyncExternalStore(
    tagTranslationService.subscribe,
    tagTranslationService.getStatus,
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

  return {
    status,
    ready: status === "ready",
    translateTag,
    translateNamespace,
    hasTranslation,
  };
}
