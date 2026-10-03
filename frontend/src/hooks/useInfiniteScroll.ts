import { useEffect, useRef } from "react";

interface UseInfiniteScrollOptions {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => void;
  rootMargin?: string;
}

export function useInfiniteScroll({
  hasNextPage,
  isFetchingNextPage,
  fetchNextPage,
  // One screen-ish instead of the old 200%: preloading two viewports ahead
  // pulled the next page's fetch + images into an active scroll.
  rootMargin = "0px 0px 800px 0px",
}: UseInfiniteScrollOptions) {
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  // Call sites pass inline arrows; keeping the latest one in a ref stops the
  // observer from being torn down and recreated on every parent render.
  const fetchRef = useRef(fetchNextPage);

  useEffect(() => {
    fetchRef.current = fetchNextPage;
  }, [fetchNextPage]);

  useEffect(() => {
    const node = sentinelRef.current;
    if (!node || !hasNextPage) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && !isFetchingNextPage) {
          fetchRef.current();
        }
      },
      { rootMargin },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [hasNextPage, isFetchingNextPage, rootMargin]);

  return sentinelRef;
}
