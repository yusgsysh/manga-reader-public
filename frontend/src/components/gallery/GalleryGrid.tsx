import { memo, useMemo } from "react";
import type { GalleryListItem } from "../../types/gallery";
import { GalleryCard } from "./GalleryCard";
import { GalleryCardSlot } from "./GallerySkeleton";
import { galleryToCard } from "./mappers";
import { useProgressiveReveal } from "../../hooks/useProgressiveReveal";

interface GalleryGridProps {
  galleries: GalleryListItem[];
}

/*
 * Memoized end to end: pages pass a `useMemo`-stabilized `galleries` array,
 * card data is mapped once per array, and `GalleryCard` is memoized — so a
 * search keystroke, an image onLoad or the tag-database notification no longer
 * re-renders every mounted card (that sync burst was dropping frames mid-scroll).
 *
 * Cards are revealed one at a time on top of that, so an appended 25-card page
 * streams in instead of landing as a block (`useProgressiveReveal`).
 */
export const GalleryGrid = memo(function GalleryGrid({
  galleries,
}: GalleryGridProps) {
  const cards = useMemo(() => galleries.map(galleryToCard), [galleries]);
  const revealed = useProgressiveReveal(cards.length);

  return (
    <div className="gallery-grid">
      {cards.map((card, index) =>
        index < revealed ? (
          <GalleryCard key={`${card.id}-${card.token}`} gallery={card} />
        ) : (
          <GalleryCardSlot key={`slot-${card.id}-${card.token}`} />
        ),
      )}
    </div>
  );
});
