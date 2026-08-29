import type { Tag } from "../../types/gallery";
import { useTagTranslation } from "../../hooks/useTagTranslation";

export interface TagBadgeProps {
  tag: Tag;
  showNamespace?: boolean;
}

export function TagBadge({ tag, showNamespace = true }: TagBadgeProps) {
  const { translateTag, translateNamespace, hasTranslation } =
    useTagTranslation();

  const translated = translateTag(tag);
  const isTranslated = hasTranslation(tag);

  if (!isTranslated) {
    return (
      <span className="inline-flex items-center rounded bg-kumo-recessed px-2 py-0.5 text-xs text-kumo-subtle opacity-60">
        {translated}
      </span>
    );
  }

  const namespaceLabel = showNamespace
    ? translateNamespace(tag.namespace)
    : "";

  return (
    <span className="inline-flex items-center gap-1 rounded bg-kumo-recessed px-2 py-0.5 text-xs text-kumo-default">
      {namespaceLabel && (
        <span className="text-kumo-inactive">[{namespaceLabel}]</span>
      )}
      <span>{translated}</span>
    </span>
  );
}
