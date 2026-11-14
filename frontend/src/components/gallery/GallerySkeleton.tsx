import { useRef } from "react";
import { useGridColumns } from "../../hooks/useGridColumns";

const BLOCK = "bg-kumo-recessed";

/*
 * One card-shaped placeholder. Used both for the initial load and inside the
 * grids while an appended page is still arriving, so the same frame is seen
 * until real content takes the cell over.
 *
 * Deliberately static: a shimmering row at the end of a list keeps animating
 * for as long as the next request takes, which reads as stuck. A calm frame
 * that holds the exact footprint is enough.
 *
 * `withPosted` mirrors whether the real card renders its posted-date line
 * (bookshelf/history cards have none), so the row height never changes when
 * the placeholder is swapped out.
 */
export function GallerySkeleton({
  withPosted = true,
}: {
  withPosted?: boolean;
}) {
  return (
    <div className="gallery-skeleton" aria-hidden>
      {/* Same cover chrome as GalleryCard (rounded-xl + hairline ring) so the
          placeholder does not visibly shift when the real cover paints in. */}
      <div
        className={`aspect-[3/4] rounded-xl ring-1 ring-kumo-hairline/70 ${BLOCK}`}
      />
      <div className="mt-2 space-y-1.5">
        <div className="min-h-10 space-y-1">
          <div className={`h-4 w-full rounded-md ${BLOCK}`} />
          <div className={`h-4 w-3/4 rounded-md ${BLOCK}`} />
        </div>
        <div className="flex items-center justify-between gap-2">
          <div className={`h-5 w-16 rounded-full ${BLOCK}`} />
          <div className={`h-4 w-8 rounded-md ${BLOCK}`} />
        </div>
        {withPosted ? (
          <div className="flex justify-end">
            <div className={`h-4 w-16 rounded-md ${BLOCK}`} />
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function GalleryGridSkeleton({ count }: { count?: number }) {
  const gridRef = useRef<HTMLDivElement>(null);
  const columns = useGridColumns(gridRef);
  const items = count ?? columns * 2;

  return (
    <div ref={gridRef} className="gallery-grid">
      {Array.from({ length: items }).map((_, i) => (
        <GallerySkeleton key={i} />
      ))}
    </div>
  );
}
