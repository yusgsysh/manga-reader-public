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
        "inline-flex items-center whitespace-nowrap rounded-[3px] border px-2 py-0.5 text-xs font-bold tracking-[1px]",
        className,
      )}
      style={categoryPillStyle(category)}
    >
      {categoryLabel(category)}
    </span>
  );
}
