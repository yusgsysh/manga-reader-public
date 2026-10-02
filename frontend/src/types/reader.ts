export interface GalleryPageThumb {
  sprite_url: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface GalleryPage {
  page_url: string;
  index: number;
  thumbnail?: GalleryPageThumb;
}

export interface Gallery {
  id: number;
  token: string;
  title: string;
  title_jpn: string;
  category: string;
  thumbnail: string;
  page_count: number;
  rating: number;
  rating_count: number;
  uploader: string;
  posted_at?: string;
  tags: { namespace: string; name: string }[];
  file_size?: string;
  expunged?: boolean;
}

export interface GalleryPagesResponse {
  id: string;
  token: string;
  total: number;
  pages: GalleryPage[];
}

// One NDJSON line of the streaming /pages endpoint.
export type GalleryPagesStreamEvent =
  | { type: "meta"; id: string; token: string; total: number }
  | ({ type: "page" } & GalleryPage)
  | { type: "done"; total: number }
  | { type: "error"; error: string };

export interface ReadingProgress {
  gallery_id: number;
  token: string;
  current_page: number;
  progress: number;
  completed: boolean;
  created_at: string | null;
  updated_at: string | null;
}

export interface UpdateReadingProgressRequest {
  current_page: number;
  progress: number;
  completed: boolean;
}
