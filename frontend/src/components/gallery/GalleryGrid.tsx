import { memo, useMemo } from "react";
import type { GalleryListItem } from "../../types/gallery";
import { GalleryCard } from "./GalleryCard";
import { galleryToCard } from "./mappers";

interface GalleryGridProps {
  galleries: GalleryListItem[];
}

/*
 * Memoized end to end: pages pass a `useMemo`-stabilized `galleries` array,
 * card data is mapped once per array, and `GalleryCard` is memoized — so a
 * search keystroke, an image onLoad or the tag-database notification no longer
 * re-renders every mounted card (that sync burst was dropping frames mid-scroll).
 */
export const GalleryGrid = memo(function GalleryGrid({
  galleries,
}: GalleryGridProps) {
  const cards = useMemo(() => galleries.map(galleryToCard), [galleries]);

  return (
    <div className="gallery-grid">
      {cards.map((card) => (
        <GalleryCard key={`${card.id}-${card.token}`} gallery={card} />
      ))}
    </div>
  );
});
