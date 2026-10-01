import type { Tag } from "../../types/gallery";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { Tooltip } from "@cloudflare/kumo";
import { Chip } from "../ui";

export interface TagBadgeProps {
  tag: Tag;
  showNamespace?: boolean;
}

export function TagBadge({ tag, showNamespace = true }: TagBadgeProps) {
  const { translateTag, translateNamespace, hasTranslation, getTagInfo } =
    useTagTranslation();

  const info = getTagInfo(tag);
  const displayName = hasTranslation(tag) ? translateTag(tag) : tag.name;
  const namespaceLabel =
    showNamespace && tag.namespace ? translateNamespace(tag.namespace) : "";
  const rawName = tag.namespace ? `${tag.namespace}:${tag.name}` : tag.name;

  const chip = (
    <Chip tone="neutral" className="font-normal">
      {namespaceLabel && (
        <span className="text-kumo-inactive">[{namespaceLabel}]</span>
      )}
      <span>{displayName}</span>
    </Chip>
  );

  if (!info) return chip;

  return (
    <Tooltip
      side="top"
      align="start"
      delay={150}
      className="cursor-help"
      render={<span />}
      content={
        <div className="max-w-xs space-y-1.5 text-left">
          <div className="font-mono text-[11px] text-kumo-subtle">{rawName}</div>
          {info.translation && (
            <div className="text-sm font-medium">{info.translation}</div>
          )}
          {info.description && (
            <div className="text-xs leading-relaxed text-kumo-subtle">
              {info.description}
            </div>
          )}
        </div>
      }
    >
      {chip}
    </Tooltip>
  );
}
