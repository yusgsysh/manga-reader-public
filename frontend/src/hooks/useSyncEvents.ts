import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { buildApiUrl } from "../api/client";

/** Query key prefixes refreshed when the backend signals a data change. */
const SYNCED_QUERY_KEYS = [
  ["reading-progress"],
  ["bookshelf"],
  ["recently-read"],
  ["gallery-cache"],
] as const;

/**
 * Subscribes to the backend's local change stream (/api/events/changes) and
 * invalidates the synced query caches whenever progress/bookshelf/gallery-cache
 * data changes — including rows applied from a remote sync peer, which the
 * local UI would otherwise never see until their staleTime expires.
 *
 * The subscription is gated on `enabled` (the caller passes authentication
 * state): an unauthenticated visitor (the login page) has no synced data to
 * invalidate, so opening the SSE stream there only wastes a request and logs a
 * 401. EventSource reconnects on its own after transient failures.
 */
export function useSyncEvents(enabled: boolean): void {
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!enabled) return;
    // EventSource cannot set an Authorization header; the HttpOnly auth
    // cookie is sent with withCredentials (needed for the dev origin).
    const source = new EventSource(buildApiUrl("/api/events/changes"), {
      withCredentials: true,
    });

    source.onmessage = () => {
      for (const queryKey of SYNCED_QUERY_KEYS) {
        void queryClient.invalidateQueries({ queryKey });
      }
    };
    // Reconnect is automatic; nothing to do on error.

    return () => source.close();
  }, [queryClient, enabled]);
}
