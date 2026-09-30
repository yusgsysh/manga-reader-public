import { GalleryCard } from "./GalleryCard";
import { readingToCard, type ReadingCardSource } from "./mappers";

interface BookshelfGridProps {
  items: ReadingCardSource[];
}

export function BookshelfGrid({ items }: BookshelfGridProps) {
  return (
    <div className="gallery-grid">
      {items.map((item) => (
        <GalleryCard
          key={`${item.id}-${item.token}`}
          gallery={readingToCard(item)}
        />
      ))}
    </div>
  );
}
