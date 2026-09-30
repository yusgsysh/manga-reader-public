import type { Tag } from "../../types/gallery";
import { Chip } from "../ui";
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
        <Chip tone="outline" className="text-kumo-inactive">
          +{remaining}
        </Chip>
      )}
    </div>
  );
}
