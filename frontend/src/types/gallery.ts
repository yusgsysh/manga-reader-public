export type GalleryCategory =
  | "doujinshi"
  | "manga"
  | "artistcg"
  | "gamecg"
  | "western"
  | "image-set"
  | "cosplay"
  | "asianporn"
  | "non-h"
  | "misc"
  | "other";

export interface GalleryListItem {
  id: number;
  token: string;
  title: string;
  category: GalleryCategory;
  cover: string;
  posted: string;
  rating: number;
  url: string;
  tags?: string[];
  uploader: string;
  pages: number;
  domain: string;
}

export interface GalleryListResponse {
  page: number;
  page_size: number;
  results: GalleryListItem[];
}

export interface SearchGalleryItem {
  id: number;
  token: string;
  title: string;
  category: GalleryCategory;
  cover: string;
  posted: string;
  rating: number;
  url: string;
  tags?: string[];
  uploader: string;
  pages: number;
  domain: string;
}

export interface SearchResponse {
  total: number;
  total_pages: number;
  page: number;
  page_size: number;
  results: SearchGalleryItem[];
}

export interface BookshelfItem {
  id: number;
  token: string;
  title: string;
  title_jpn: string;
  category: GalleryCategory;
  thumbnail: string;
  pages: number;
  added_at: string;
  updated_at: string;
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

export interface BookshelfListResponse {
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
  results: BookshelfItem[];
}

export interface GalleryDetail {
  id: number;
  token: string;
  domain: string;
  title: string;
  title_jpn: string;
  cover: string;
  category: GalleryCategory;
  uploader: string;
  posted: string;
  parent: number;
  visible: string;
  language: string;
  translated: boolean;
  file_size: string;
  page_count: number;
  favorited: number;
  rating_count: number;
  rating: number;
  tags: Tag[];
  page_urls: string[];
}

export interface Tag {
  namespace: string;
  name: string;
}

export interface ApiError {
  error: string;
}
