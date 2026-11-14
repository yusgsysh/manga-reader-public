import { memo, useMemo } from "react";
import { GalleryCard } from "./GalleryCard";
import { readingToCard, type ReadingCardSource } from "./mappers";

interface BookshelfGridProps {
  items: ReadingCardSource[];
}

export const BookshelfGrid = memo(function BookshelfGrid({
  items,
}: BookshelfGridProps) {
  const cards = useMemo(() => items.map(readingToCard), [items]);

  return (
    <div className="gallery-grid">
      {cards.map((card) => (
        <GalleryCard key={`${card.id}-${card.token}`} gallery={card} />
      ))}
    </div>
  );
});
