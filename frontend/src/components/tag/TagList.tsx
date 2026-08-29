import type { Tag } from "../../types/gallery";
import { TagBadge } from "./TagBadge";

export interface TagListProps {
  tags: Tag[];
  max?: number;
}

export function TagList({ tags, max }: TagListProps) {
  const safeTags = tags ?? [];
  const shown = max ? safeTags.slice(0, max) : safeTags;
  const remaining = max ? safeTags.length - max : 0;

  return (
    <div className="flex flex-wrap gap-1.5">
      {shown.map((tag) => (
        <TagBadge key={`${tag.namespace}:${tag.name}`} tag={tag} />
      ))}
      {remaining > 0 && (
        <span className="inline-flex items-center rounded bg-kumo-recessed px-2 py-0.5 text-xs text-kumo-inactive">
          +{remaining}
        </span>
      )}
    </div>
  );
}
