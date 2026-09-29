import { apiGet } from "./client";
import type { RecentlyReadResponse } from "../types/recentlyRead";

export function fetchRecentlyRead(
  page = 0,
): Promise<RecentlyReadResponse> {
  return apiGet<RecentlyReadResponse>("/api/recently-read", { page });
}
