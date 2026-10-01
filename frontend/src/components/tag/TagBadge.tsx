import { useState } from "react";
import type { Tag } from "../../types/gallery";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { useMediaQuery } from "../../hooks/useMediaQuery";
import { Popover, Tooltip } from "@cloudflare/kumo";
import { Chip } from "../ui";

export interface TagBadgeProps {
  tag: Tag;
  showNamespace?: boolean;
}

export function TagBadge({ tag, showNamespace = true }: TagBadgeProps) {
  const { translateTag, translateNamespace, hasTranslation, getTagInfo } =
    useTagTranslation();
  const noHover = useMediaQuery("(hover: none)");
  const [open, setOpen] = useState(false);

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

  const detail = (
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
  );

  // Touch/no-hover devices cannot open the hover-only Tooltip, so tap to open
  // a Popover with the same details. Desktop keeps the hover tooltip.
  if (noHover) {
    return (
      <Popover open={open} onOpenChange={setOpen}>
        <Popover.Trigger
          render={<button type="button" className="text-left" />}
        >
          {chip}
        </Popover.Trigger>
        <Popover.Content
          side="top"
          align="start"
          sideOffset={8}
          className="p-3"
        >
          {detail}
        </Popover.Content>
      </Popover>
    );
  }

  return (
    <Tooltip
      side="top"
      align="start"
      delay={150}
      render={<span />}
      content={detail}
    >
      {chip}
    </Tooltip>
  );
}
