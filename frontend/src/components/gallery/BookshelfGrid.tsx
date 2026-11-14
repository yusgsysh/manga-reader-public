import { memo, useMemo } from "react";
import { GalleryCard } from "./GalleryCard";
import { GalleryCardSlot } from "./GallerySkeleton";
import { readingToCard, type ReadingCardSource } from "./mappers";
import { useProgressiveReveal } from "../../hooks/useProgressiveReveal";

interface BookshelfGridProps {
  items: ReadingCardSource[];
}

export const BookshelfGrid = memo(function BookshelfGrid({
  items,
}: BookshelfGridProps) {
  const cards = useMemo(() => items.map(readingToCard), [items]);
  // Same paced reveal as the listing grids: an appended page of history or
  // shelf entries flows in instead of appearing all at once.
  const revealed = useProgressiveReveal(cards.length);

  return (
    <div className="gallery-grid">
      {cards.map((card, index) =>
        index < revealed ? (
          <GalleryCard key={`${card.id}-${card.token}`} gallery={card} />
        ) : (
          <GalleryCardSlot
            key={`slot-${card.id}-${card.token}`}
            withPosted={false}
          />
        ),
      )}
    </div>
  );
});
