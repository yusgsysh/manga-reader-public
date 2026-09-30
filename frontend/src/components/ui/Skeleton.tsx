import { cn } from "@cloudflare/kumo";

interface SkeletonProps {
  className?: string;
}

export function Skeleton({ className }: SkeletonProps) {
  return (
    <div
      aria-hidden
      className={cn(
        "skeleton-shimmer rounded-md bg-kumo-recessed",
        className,
      )}
    />
  );
}
