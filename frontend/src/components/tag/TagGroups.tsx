import { useMemo } from "react";
import type { Tag } from "../../types/gallery";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import {
  useTagPreferences,
  sortTagsByPreference,
} from "../../hooks/useTagPreferences";
import { TagBadge } from "./TagBadge";

interface TagGroupsProps {
  tags: Tag[];
}

// Renders tags grouped by namespace, preserving the order in which the
// namespaces first appear. Within each group, liked (blue) tags sort before
// disliked (red) ones and neutral tags last. Each group is headed by the
// translated namespace name, so the badges themselves omit the [namespace]
// prefix.
export function TagGroups({ tags }: TagGroupsProps) {
  const { translateNamespace } = useTagTranslation();
  const { getTagAction } = useTagPreferences();

  const groups = useMemo(() => {
    const byNamespace = new Map<string, Tag[]>();
    for (const tag of tags ?? []) {
      const namespace = tag.namespace || "other";
      const group = byNamespace.get(namespace);
      if (group) group.push(tag);
      else byNamespace.set(namespace, [tag]);
    }
    return [...byNamespace.entries()].map(([namespace, groupTags]) => [
      namespace,
      sortTagsByPreference(groupTags, getTagAction),
    ] as const);
  }, [tags, getTagAction]);

  if (groups.length === 0) return null;

  return (
    <div className="space-y-4">
      {groups.map(([namespace, groupTags]) => (
        <div key={namespace} className="space-y-1.5">
          <h3 className="text-xs font-medium text-kumo-subtle">
            {translateNamespace(namespace) || namespace}
          </h3>
          <div className="flex flex-wrap gap-1.5">
            {groupTags.map((tag) => (
              <TagBadge
                key={`${tag.namespace}:${tag.name}`}
                tag={tag}
                showNamespace={false}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
