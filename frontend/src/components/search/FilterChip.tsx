import { X } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";

export function FilterChip({
  label,
  onRemove,
  className,
}: {
  label: string;
  onRemove: () => void;
  /** Extra classes (e.g. preference colours) merged over the base style. */
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full bg-[var(--app-chip-bg)] px-2.5 py-1 text-xs font-medium text-[var(--app-chip-fg)]",
        className,
      )}
    >
      {label}
      <button
        type="button"
        onClick={onRemove}
        aria-label={`移除筛选 ${label}`}
        className="transition-opacity hover:opacity-70"
      >
        <X className="size-3" weight="bold" />
      </button>
    </span>
  );
}
