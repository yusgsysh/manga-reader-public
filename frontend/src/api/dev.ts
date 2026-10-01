import { apiGet, apiPut } from "./client";

// Debug-only endpoint, registered by the backend when MANGA_READER_DEV_TOOLS
// is enabled. When the tools are off it answers 404, which callers surface as
// "not enabled".
interface UpstreamDownState {
  down: boolean;
}

export function fetchUpstreamDown(): Promise<UpstreamDownState> {
  return apiGet<UpstreamDownState>("/api/dev/upstream-down");
}

export function setUpstreamDown(down: boolean): Promise<UpstreamDownState> {
  return apiPut<UpstreamDownState>("/api/dev/upstream-down", { down });
}
