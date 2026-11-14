import { useEffect, useState } from "react";
import { Skeleton } from "../ui";

function columnsForWidth(width: number): number {
  if (width < 480) return 2;
  if (width < 768) return 3;
  if (width < 1024) return 4;
  if (width < 1440) return 5;
  if (width < 1920) return 6;
  return 7;
}

function useGridColumnCount(): number {
  const [columns, setColumns] = useState(() =>
    columnsForWidth(window.innerWidth),
  );

  useEffect(() => {
    const onResize = () => setColumns(columnsForWidth(window.innerWidth));
    window.addEventListener("resize", onResize, { passive: true });
    return () => window.removeEventListener("resize", onResize);
  }, []);

  return columns;
}

function GallerySkeleton() {
  return (
    <div>
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
        <div className="flex justify-end">
          <Skeleton className="h-4 w-16" />
        </div>
      </div>
    </div>
  );
}

/*
 * Invisible stand-in with exactly one card's footprint. The grids render one
 * per queued card, so the list keeps its final height while cards stream in:
 * the infinite-scroll sentinel stays at the real bottom (no eager prefetch of
 * the page after next) and the document does not grow one row at a time.
 * It paints nothing on purpose — the batch is already in memory, so a shimmer
 * would be a lie.
 */
export function GalleryCardSlot({
  withPosted = true,
}: {
  withPosted?: boolean;
}) {
  return (
    <div className="gallery-slot" aria-hidden>
      <div className="aspect-[3/4] rounded-xl" />
      {/* Mirrors GalleryCard's text block so the row height never changes when
          the real card takes the slot over. Keep in sync with GalleryCard. */}
      <div className="mt-2 space-y-1.5">
        <div className="min-h-10" />
        <div className="h-5" />
        {withPosted ? <div className="h-4" /> : null}
      </div>
    </div>
  );
}

export function GalleryGridSkeleton({ count }: { count?: number }) {
  const columns = useGridColumnCount();
  const items = count ?? columns * 2;

  return (
    <div className="gallery-grid">
      {Array.from({ length: items }).map((_, i) => (
        <GallerySkeleton key={i} />
      ))}
    </div>
  );
}
