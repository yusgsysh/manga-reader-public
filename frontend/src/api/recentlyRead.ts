import { apiGet } from "./client";
import type { RecentlyReadResponse } from "../types/recentlyRead";

export function fetchRecentlyRead(): Promise<RecentlyReadResponse> {
  return apiGet<RecentlyReadResponse>("/api/recently-read");
}
