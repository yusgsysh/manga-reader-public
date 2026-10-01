import { buildApiUrl } from "../api/client";
import type { GalleryPageThumb } from "../types/reader";

// MinIO-backed image-cache endpoints. The reader loads every image through
// these so repeat views (and the reader's many page thumbnails) stay cheap.

export function thumbnailUrl(url: string): string {
  if (!url) return "";
  return buildApiUrl("/api/image-cache/thumbnail", { url });
}

export function pageImageUrl(url: string): string {
  return buildApiUrl("/api/image-cache/page", { url });
}

export function pageThumbnailUrl(thumb: GalleryPageThumb): string {
  return buildApiUrl("/api/image-cache/page-thumbnail", {
    url: thumb.sprite_url,
    x: thumb.x,
    y: thumb.y,
    w: thumb.width,
    h: thumb.height,
  });
}
