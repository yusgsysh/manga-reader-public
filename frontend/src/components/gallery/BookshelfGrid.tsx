import type { BookshelfItem } from "../../types/gallery";
import { BookshelfCard } from "./BookshelfCard";

interface BookshelfGridProps {
  items: BookshelfItem[];
}

export function BookshelfGrid({ items }: BookshelfGridProps) {
  return (
    <div className="gallery-grid">
      {items.map((item) => (
        <BookshelfCard key={`${item.id}-${item.token}`} item={item} />
      ))}
    </div>
  );
}
