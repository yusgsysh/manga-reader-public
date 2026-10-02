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

export interface ListingNav {
  prev: string;
  next: string;
  min_date: string;
  max_date: string;
  range_min: number;
  range_max: number;
  range_span: number;
}

export interface ListingNavOptions {
  seek?: string;
  jump?: string;
}

export interface GalleryListResponse {
  page: number;
  page_size: number;
  results: GalleryListItem[];
  nav?: ListingNav;
}

export interface SearchResponse {
  total: number;
  total_pages: number;
  page: number;
  page_size: number;
  results: GalleryListItem[];
  nav?: ListingNav;
}

export interface AdvancedSearchOptions {
  min_pages?: number;
  max_pages?: number;
  min_rating?: number;
  has_torrent?: boolean;
  include_expunged?: boolean;
  search_name?: boolean;
  search_tags?: boolean;
  search_description?: boolean;
  include_low_power_tags?: boolean;
  include_downvoted_tags?: boolean;
  disable_language_filter?: boolean;
  disable_uploader_filter?: boolean;
  disable_tag_filter?: boolean;
}

export interface GalleryListFilters extends AdvancedSearchOptions {
  tags?: string[];
  categories?: string[];
}

export interface SearchParams extends AdvancedSearchOptions, ListingNavOptions {
  q?: string;
  site?: string;
  categories?: string;
  page?: number;
}

export interface BookshelfItem {
  id: number;
  token: string;
  title: string;
  title_jpn: string;
  category: GalleryCategory;
  thumbnail: string;
  pages: number;
  created_at: string;
  updated_at: string;
  reading?: {
    gallery_id: number;
    token: string;
    current_page: number;
    progress: number;
    completed: boolean;
    created_at?: string;
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
  torrent_count?: number;
  tags: Tag[];
}

export interface GalleryTorrent {
  gtid: string;
  name: string;
  size: string;
  posted: string;
  seeds: number;
  peers: number;
  downloads: number;
  uploader: string;
}

export interface GalleryTorrentInfo {
  posted: string;
  seeds: number;
  uploader: string;
  dlers: number;
  size: string;
  completes: number;
  comments: string;
  personalized: boolean;
}

export interface Tag {
  namespace: string;
  name: string;
}

export interface ApiError {
  error: string;
}
