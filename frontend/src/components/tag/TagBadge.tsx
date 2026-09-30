import type { Tag } from "../../types/gallery";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { Chip } from "../ui";

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
    <Chip tone="neutral" className="font-normal">
      {namespaceLabel && <span className="text-kumo-inactive">[{namespaceLabel}]</span>}
      <span>{displayName}</span>
    </Chip>
  );
}
