import { memo, useMemo, useRef } from "react";
import { GalleryCard } from "./GalleryCard";
import { GallerySkeleton } from "./GallerySkeleton";
import { useGridColumns } from "../../hooks/useGridColumns";
import { readingToCard, type ReadingCardSource } from "./mappers";

interface BookshelfGridProps {
  items: ReadingCardSource[];
  /** The next page is in flight: hold its row with skeleton cards. */
  loadingMore?: boolean;
}

export const BookshelfGrid = memo(function BookshelfGrid({
  items,
  loadingMore = false,
}: BookshelfGridProps) {
  const cards = useMemo(() => items.map(readingToCard), [items]);
  const gridRef = useRef<HTMLDivElement>(null);
  const columns = useGridColumns(gridRef);

  return (
    <div ref={gridRef} className="gallery-grid">
      {cards.map((card) => (
        <GalleryCard key={`${card.id}-${card.token}`} gallery={card} />
      ))}
      {/* Shelf/history cards have no posted line, so their skeletons skip it
          too and the row height stays identical when the cards land. */}
      {loadingMore
        ? Array.from({ length: columns }).map((_, index) => (
            <GallerySkeleton
              key={`pending-${index}`}
              className={index === 0 ? "col-start-1" : undefined}
              withPosted={false}
            />
          ))
        : null}
    </div>
  );
});
