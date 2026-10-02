import { X } from "@phosphor-icons/react";

export function FilterChip({
  label,
  onRemove,
}: {
  label: string;
  onRemove: () => void;
}) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-[var(--app-chip-bg)] px-2.5 py-1 text-xs font-medium text-[var(--app-chip-fg)]">
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
