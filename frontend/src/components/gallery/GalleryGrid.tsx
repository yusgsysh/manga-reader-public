import type { GalleryListItem } from "../../types/gallery";
import { GalleryCard } from "./GalleryCard";

interface GalleryGridProps {
  galleries: GalleryListItem[];
}

export function GalleryGrid({ galleries }: GalleryGridProps) {
  return (
    <div className="gallery-grid">
      {galleries.map((gallery) => (
        <GalleryCard key={`${gallery.id}-${gallery.token}`} gallery={gallery} />
      ))}
    </div>
  );
}
