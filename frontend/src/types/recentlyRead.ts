import type { GalleryCategory } from "./gallery";

export interface RecentlyReadItem {
  id: number;
  token: string;
  title: string;
  title_jpn: string;
  category: GalleryCategory;
  thumbnail: string;
  pages: number;
  reading?: {
    gallery_id: number;
    token: string;
    current_page: number;
    progress: number;
    completed: boolean;
    started_at?: string;
    updated_at?: string;
  };
}

export interface RecentlyReadResponse {
  results: RecentlyReadItem[];
}
