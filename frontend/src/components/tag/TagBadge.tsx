import type { Tag } from "../../types/gallery";
import { useTagTranslation } from "../../hooks/useTagTranslation";

export interface TagBadgeProps {
  tag: Tag;
  showNamespace?: boolean;
}

export function TagBadge({ tag, showNamespace = true }: TagBadgeProps) {
  const { translateTag, translateNamespace, hasTranslation } =
    useTagTranslation();

  const displayName = hasTranslation(tag) ? translateTag(tag) : tag.name;
  const namespaceLabel =
    showNamespace && tag.namespace ? translateNamespace(tag.namespace) : "";

  return (
    <span className="inline-flex items-center gap-1 rounded bg-kumo-recessed px-2 py-0.5 text-xs font-bold text-kumo-subtle">
      {namespaceLabel && <span>[{namespaceLabel}]</span>}
      <span>{displayName}</span>
    </span>
  );
}
