import { memo, useMemo } from "react";
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
 * While the next page is in flight, its row is held by card-shaped skeletons,
 * so scrolling to the end never runs into blank space and the append lands in
 * space that is already the right size.
 */
export const GalleryGrid = memo(function GalleryGrid({
  galleries,
  loadingMore = false,
}: GalleryGridProps) {
  const cards = useMemo(() => galleries.map(galleryToCard), [galleries]);
  const columns = useGridColumns();

  return (
    <div className="gallery-grid">
      {cards.map((card) => (
        <GalleryCard key={`${card.id}-${card.token}`} gallery={card} />
      ))}
      {loadingMore
        ? Array.from({ length: columns }).map((_, index) => (
            <GallerySkeleton key={`pending-${index}`} />
          ))
        : null}
    </div>
  );
});
