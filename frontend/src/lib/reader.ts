import type { ReactManga } from "@yui540/comimi-react";
import { pageImageUrl, pageThumbnailUrl } from "./image";
import type { GalleryPage } from "../types/reader";

export function getReaderImageURL(pageURL: string): string {
  return pageImageUrl(pageURL);
}

export function galleryPagesToManga(
  id: string,
  token: string,
  title: string,
  pages: GalleryPage[],
): ReactManga {
  return {
    id: `${id}:${token}`,
    title,
    pages: pages.map((page) => ({
      id: `${page.index}`,
      type: "image" as const,
      src: getReaderImageURL(page.page_url),
      thumbnailSrc: page.thumbnail ? pageThumbnailUrl(page.thumbnail) : undefined,
      alt: `${title} - ${page.index + 1}`,
    })),
  };
}

export function clampPageIndex(pageIndex: number, total: number): number {
  if (total <= 0) return 0;
  return Math.min(Math.max(Math.round(pageIndex), 0), total - 1);
}

export function calculateProgress(
  currentPage: number,
  total: number,
): { progress: number; completed: boolean } {
  if (total <= 0) return { progress: 0, completed: false };
  const isLastPage = currentPage >= total - 1;
  if (isLastPage) return { progress: 1, completed: true };
  return {
    progress: Math.min(Math.max((currentPage + 1) / total, 0), 1),
    completed: false,
  };
}
