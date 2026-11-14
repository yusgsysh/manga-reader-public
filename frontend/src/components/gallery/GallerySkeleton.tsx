import { Skeleton } from "../ui";
import { useGridColumns } from "../../hooks/useGridColumns";

/*
 * One card-shaped placeholder. Used both for the initial load and inside the
 * grids while an appended page is still arriving, so the same shimmering
 * frame is seen until real content takes the cell over.
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
    <div className="gallery-skeleton">
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
  const columns = useGridColumns();
  const items = count ?? columns * 2;

  return (
    <div className="gallery-grid">
      {Array.from({ length: items }).map((_, i) => (
        <GallerySkeleton key={i} />
      ))}
    </div>
  );
}
