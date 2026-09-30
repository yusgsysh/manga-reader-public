import type { GalleryListItem } from "../../types/gallery";
import { GalleryCard } from "./GalleryCard";
import { galleryToCard } from "./mappers";

interface GalleryGridProps {
  galleries: GalleryListItem[];
}

export function GalleryGrid({ galleries }: GalleryGridProps) {
  return (
    <div className="gallery-grid">
      {galleries.map((gallery) => (
        <GalleryCard
          key={`${gallery.id}-${gallery.token}`}
          gallery={galleryToCard(gallery)}
        />
      ))}
    </div>
  );
}
