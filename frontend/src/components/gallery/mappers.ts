import type { BookshelfItem, GalleryListItem } from "../../types/gallery";
import type { GalleryCardData } from "./GalleryCard";

export interface ReadingCardSource {
  id: number;
  token: string;
  title: string;
  category: BookshelfItem["category"];
  thumbnail: string;
  pages: number;
  reading?: BookshelfItem["reading"];
}

export function galleryToCard(gallery: GalleryListItem): GalleryCardData {
  return {
    id: gallery.id,
    token: gallery.token,
    title: gallery.title,
    category: gallery.category,
    image: gallery.cover,
    rating: gallery.rating,
    pages: gallery.pages,
    posted: gallery.posted,
    tags: gallery.tags,
  };
}

export function readingToCard(item: ReadingCardSource): GalleryCardData {
  return {
    id: item.id,
    token: item.token,
    title: item.title,
    category: item.category,
    image: item.thumbnail,
    pages: item.pages,
    reading: item.reading
      ? { progress: item.reading.progress, completed: item.reading.completed }
      : undefined,
  };
}
