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
}

export interface UseOnlineThenCachedResult<T> {
  data: T | undefined;
  source: DataSource | null;
  isLoading: boolean;
  // True while the online query is in flight (including streaming responses).
  isFetching: boolean;
  isError: boolean;
  error: Error | null;
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
}: UseOnlineThenCachedOptions<T>): UseOnlineThenCachedResult<T> {
  const online = useOnlineStatus();

  const onlineQuery = useQuery({
    queryKey: onlineKey,
    queryFn: (context) => onlineFn(context),
    enabled: enabled && online,
    staleTime,
    retry: onlineRetry,
  });

  const useCache = enabled && (!online || onlineQuery.isError);
  const cacheQuery = useQuery({
    queryKey: cacheKey,
    queryFn: cacheFn,
    enabled: useCache,
    staleTime,
    retry: 0,
  });

  const data = onlineQuery.data ?? cacheQuery.data;
  const source: DataSource | null =
    onlineQuery.data !== undefined
      ? "online"
      : cacheQuery.data !== undefined
        ? "cache"
        : null;

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
    refetch,
  };
}
