import type { ReactNode } from "react";
import { cn } from "@cloudflare/kumo";

type ChipTone = "neutral" | "solid" | "outline";

interface ChipProps {
  children: ReactNode;
  tone?: ChipTone;
  className?: string;
}

const TONE_CLASSES: Record<ChipTone, string> = {
  neutral: "bg-kumo-recessed text-kumo-subtle",
  solid: "bg-kumo-contrast text-kumo-inverse",
  outline: "border border-kumo-hairline text-kumo-subtle",
};

export function Chip({ children, tone = "neutral", className }: ChipProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        TONE_CLASSES[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
