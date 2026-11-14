import { memo, useMemo, useRef } from "react";
import type { GalleryListItem } from "../../types/gallery";
import { GalleryCard } from "./GalleryCard";
import { GallerySkeleton } from "./GallerySkeleton";
import { useGridColumns } from "../../hooks/useGridColumns";
import { galleryToCard } from "./mappers";

interface GalleryGridProps {
  galleries: GalleryListItem[];
  /** The next page is in flight: hold its row with skeleton cards. */
  loadingMore?: boolean;
}

/*
 * Memoized end to end: pages pass a `useMemo`-stabilized `galleries` array,
 * card data is mapped once per array, and `GalleryCard` is memoized — so a
 * search keystroke, an image onLoad or the tag-database notification no longer
 * re-renders every mounted card (that sync burst was dropping frames mid-scroll).
 *
 * While the next page is in flight its place is held by one row of shimmering
 * skeleton cards. The first one is pinned to column 1, so the placeholder row
 * starts on a fresh line instead of filling the free cells of the last real
 * row (that "extra card" was what made the page grow and then snap back).
 */
export const GalleryGrid = memo(function GalleryGrid({
  galleries,
  loadingMore = false,
}: GalleryGridProps) {
  const cards = useMemo(() => galleries.map(galleryToCard), [galleries]);
  const gridRef = useRef<HTMLDivElement>(null);
  const columns = useGridColumns(gridRef);

  return (
    <div ref={gridRef} className="gallery-grid">
      {cards.map((card) => (
        <GalleryCard key={`${card.id}-${card.token}`} gallery={card} />
      ))}
      {loadingMore
        ? Array.from({ length: columns }).map((_, index) => (
            <GallerySkeleton
              key={`pending-${index}`}
              className={index === 0 ? "col-start-1" : undefined}
            />
          ))
        : null}
    </div>
  );
});
