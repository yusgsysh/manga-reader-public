import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { buildApiUrl } from "../api/client";

/** Query key prefixes refreshed when the backend signals a data change. */
const SYNCED_QUERY_KEYS = [
  ["reading-progress"],
  ["bookshelf"],
  ["recently-read"],
] as const;

/**
 * Subscribes to the backend's local change stream (/api/events/changes) and
 * invalidates the synced query caches whenever progress/bookshelf data
 * changes — including rows applied from a remote sync peer, which the local
 * UI would otherwise never see until their staleTime expires.
 *
 * EventSource reconnects on its own after transient failures.
 */
export function useSyncEvents(): void {
  const queryClient = useQueryClient();

  useEffect(() => {
    const source = new EventSource(buildApiUrl("/api/events/changes"));

    source.onmessage = () => {
      for (const queryKey of SYNCED_QUERY_KEYS) {
        void queryClient.invalidateQueries({ queryKey });
      }
    };
    // Reconnect is automatic; nothing to do on error.

    return () => source.close();
  }, [queryClient]);
}
