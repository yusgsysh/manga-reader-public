import { cn } from "@cloudflare/kumo";
import { categoryLabel, categoryPillStyle } from "../../lib/categories";

interface CategoryChipProps {
  category?: string;
  className?: string;
}

export function CategoryChip({ category, className }: CategoryChipProps) {
  if (!category) return null;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium",
        className,
      )}
      style={categoryPillStyle(category)}
    >
      {categoryLabel(category)}
    </span>
  );
}
