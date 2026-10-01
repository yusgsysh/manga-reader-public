import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";

export type DataSource = "online" | "cache";

export function useOnlineStatus(): boolean {
  const [online, setOnline] = useState(() =>
    typeof navigator === "undefined" ? true : navigator.onLine,
  );

  useEffect(() => {
    const handleOnline = () => setOnline(true);
    const handleOffline = () => setOnline(false);
    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    return () => {
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
    };
  }, []);

  return online;
}

interface UseOnlineThenCachedOptions<T> {
  enabled: boolean;
  onlineKey: readonly unknown[];
  cacheKey: readonly unknown[];
  onlineFn: (context: { signal: AbortSignal }) => Promise<T>;
  cacheFn: () => Promise<T>;
  staleTime?: number;
  onlineRetry?: number;
  /**
   * When true, the cache is queried immediately (even while online) and shown
   * as a placeholder until the online result arrives. The online query still
   * runs, so online stays authoritative and the fallback semantics are
   * unchanged; the only difference is that a cache hit renders instantly
   * instead of waiting for the online request (which may be slow or fail).
   */
  useCacheAsPlaceholder?: boolean;
}

export interface UseOnlineThenCachedResult<T> {
  data: T | undefined;
  source: DataSource | null;
  isLoading: boolean;
  // True while the online query is in flight (including streaming responses).
  isFetching: boolean;
  isError: boolean;
  error: Error | null;
  /**
   * True when the cache is shown because the online source failed or the
   * browser is offline (a real fallback), not as a placeholder while the
   * online query is still running.
   */
  isFallback: boolean;
  refetch: () => void;
}

/**
 * Tries the online endpoint first (with retries). Once it fails, or when the
 * browser is offline, the read-only cache endpoint is used as a fallback. The
 * whole retry/fallback orchestration lives here on the frontend.
 */
export function useOnlineThenCached<T>({
  enabled,
  onlineKey,
  cacheKey,
  onlineFn,
  cacheFn,
  staleTime,
  onlineRetry = 2,
  useCacheAsPlaceholder = false,
}: UseOnlineThenCachedOptions<T>): UseOnlineThenCachedResult<T> {
  const online = useOnlineStatus();

  const onlineQuery = useQuery({
    queryKey: onlineKey,
    queryFn: (context) => onlineFn(context),
    enabled: enabled && online,
    staleTime,
    retry: onlineRetry,
  });

  const useCache =
    enabled && (useCacheAsPlaceholder || !online || onlineQuery.isError);
  const cacheQuery = useQuery({
    queryKey: cacheKey,
    queryFn: cacheFn,
    enabled: useCache,
    staleTime,
    retry: 0,
  });

  const onlineData = onlineQuery.data;
  const cacheData = cacheQuery.data;
  // Online stays authoritative; the cache is only a placeholder until it wins
  // (or a fallback when online never produces data).
  const data = onlineData ?? cacheData;
  const source: DataSource | null =
    onlineData !== undefined
      ? "online"
      : cacheData !== undefined
        ? "cache"
        : null;
  const isFallback = source === "cache" && (!online || onlineQuery.isError);

  const onlineErrored = !enabled || !online || onlineQuery.isError;
  const cacheErrored = !useCache || cacheQuery.isError;
  const isError = enabled && data === undefined && onlineErrored && cacheErrored;
  const isLoading = enabled && data === undefined && !isError;

  const refetch = () => {
    if (!online) {
      void cacheQuery.refetch();
      return;
    }
    void onlineQuery.refetch();
    if (useCacheAsPlaceholder) void cacheQuery.refetch();
  };

  // Only surface an error when neither source produced data; a successful
  // cache fallback must not leak the (expected) online failure to callers.
  const error = isError
    ? ((onlineQuery.error ?? cacheQuery.error) as Error | null)
    : null;

  return {
    data,
    source,
    isLoading,
    isFetching: onlineQuery.isFetching,
    isError,
    error,
    isFallback,
    refetch,
  };
}
