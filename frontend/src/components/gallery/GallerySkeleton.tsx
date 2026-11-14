import { useRef } from "react";
import { Skeleton } from "../ui";
import { useGridColumns } from "../../hooks/useGridColumns";

/*
 * One shimmering card-shaped frame. Used for the initial load and, inside the
 * grids, while the next page is in flight.
 *
 * `withPosted` mirrors whether the real card renders its posted-date line
 * (bookshelf/history cards have none), so the row height never changes when
 * the placeholder is swapped for the real card.
 */
export function GallerySkeleton({
  withPosted = true,
  className,
}: {
  withPosted?: boolean;
  className?: string;
}) {
  return (
    <div className={className} aria-hidden>
      {/* Same cover chrome as GalleryCard (rounded-xl + hairline ring) so the
          placeholder does not visibly shift when the real cover paints in. */}
      <Skeleton className="aspect-[3/4] rounded-xl ring-1 ring-kumo-hairline/70" />
      <div className="mt-2 space-y-1.5">
        <div className="min-h-10 space-y-1">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
        </div>
        <div className="flex items-center justify-between gap-2">
          <Skeleton className="h-5 w-16 rounded-full" />
          <Skeleton className="h-4 w-8" />
        </div>
        {withPosted ? (
          <div className="flex justify-end">
            <Skeleton className="h-4 w-16" />
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function GalleryGridSkeleton({ count }: { count?: number }) {
  const gridRef = useRef<HTMLDivElement>(null);
  const columns = useGridColumns(gridRef);

  return (
    <div ref={gridRef} className="gallery-grid">
      {Array.from({ length: count ?? columns * 2 }).map((_, i) => (
        <GallerySkeleton key={i} />
      ))}
    </div>
  );
}
