import { useState } from "react";
import type { Tag } from "../../types/gallery";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { useTagPreferences, TAG_ACTION_CHIP_CLASS } from "../../hooks/useTagPreferences";
import { useMediaQuery } from "../../hooks/useMediaQuery";
import { Popover, Tooltip, cn } from "@cloudflare/kumo";
import { Chip } from "../ui";

interface TagBadgeProps {
  tag: Tag;
  showNamespace?: boolean;
}

export function TagBadge({ tag, showNamespace = true }: TagBadgeProps) {
  const { translateTag, translateNamespace, hasTranslation, getTagInfo } =
    useTagTranslation();
  const { getTagRule } = useTagPreferences();
  const noHover = useMediaQuery("(hover: none)");
  const [open, setOpen] = useState(false);

  const info = getTagInfo(tag);
  const displayName = hasTranslation(tag) ? translateTag(tag) : tag.name;
  const namespaceLabel =
    showNamespace && tag.namespace ? translateNamespace(tag.namespace) : "";
  const rawName = tag.namespace ? `${tag.namespace}:${tag.name}` : tag.name;

  // Solid blue (liked) / red (disliked) chips matching the source My Tags
  // colours; neutral tags keep the default chip styling.
  const rule = getTagRule(tag);
  const chipClass = rule ? TAG_ACTION_CHIP_CLASS[rule.action] : null;

  const chip = (
    <Chip tone="neutral" className={cn("font-normal", chipClass)}>
      {namespaceLabel && (
        <span className={chipClass ? "opacity-70" : "text-kumo-inactive"}>
          [{namespaceLabel}]
        </span>
      )}
      <span>{displayName}</span>
    </Chip>
  );

  if (!info) return chip;

  const detail = (
    <div className="max-w-xs space-y-1.5 text-left">
      <div
        className={cn(
          "inline-block rounded px-1.5 py-0.5 font-mono text-[11px]",
          chipClass ?? "text-kumo-subtle",
        )}
      >
        {rawName}
      </div>
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
