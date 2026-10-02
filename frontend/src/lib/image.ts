import { buildApiUrl } from "../api/client";

// MinIO-backed image-cache endpoints. The reader loads every image through
// these so repeat views (and the reader's many page thumbnails) stay cheap.

export function thumbnailUrl(url: string): string {
  if (!url) return "";
  return buildApiUrl("/api/image-cache/thumbnail", { url });
}

export function pageImageUrl(url: string): string {
  return buildApiUrl("/api/image-cache/page", { url });
}

// Address the crop by gallery page index instead of sprite URL + rectangle:
// the backend resolves the sprite geometry itself (gallery cache first).
export function pageThumbnailUrl(
  id: number | string,
  token: string,
  index: number,
): string {
  return buildApiUrl("/api/image-cache/page-thumbnail", {
    id,
    token,
    index,
  });
}
