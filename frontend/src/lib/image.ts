import { buildApiUrl } from "../api/client";

export function thumbnailUrl(url: string): string {
  if (!url) return "";
  return buildApiUrl("/api/cached-thumbnail", { url });
}
