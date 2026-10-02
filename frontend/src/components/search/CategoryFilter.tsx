import { cn } from "@cloudflare/kumo";
import { categoryPillStyle } from "../../lib/categories";
import { CATEGORIES } from "./advancedSearch";

interface CategoryFilterProps {
  included: string[];
  onToggle: (value: string) => void;
  disabled?: boolean;
  className?: string;
}

export function CategoryFilter({
  included,
  onToggle,
  disabled,
  className,
}: CategoryFilterProps) {
  return (
    <div className={cn("flex flex-wrap gap-2", className)}>
      {CATEGORIES.map((cat) => {
        const active = included.includes(cat.value);
        return (
          <button
            key={cat.value}
            type="button"
            onClick={() => onToggle(cat.value)}
            aria-pressed={active}
            disabled={disabled}
            style={active ? categoryPillStyle(cat.value) : undefined}
            className={cn(
              "rounded-full border px-3 py-1 text-xs font-medium transition-colors disabled:opacity-50",
              active
                ? "border-transparent"
                : "border-kumo-hairline text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default",
            )}
          >
            {cat.label}
          </button>
        );
      })}
    </div>
  );
}
