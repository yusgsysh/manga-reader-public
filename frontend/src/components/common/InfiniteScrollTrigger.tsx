import { Button } from "@cloudflare/kumo";
import { useInfiniteScroll } from "../../hooks/useInfiniteScroll";

interface InfiniteScrollTriggerProps {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  fetchNextPage: () => void;
  isFetchNextPageError?: boolean;
  endMessage?: string;
}

export function InfiniteScrollTrigger({
  hasNextPage,
  isFetchingNextPage,
  fetchNextPage,
  isFetchNextPageError,
  endMessage = "已经到底了",
}: InfiniteScrollTriggerProps) {
  const sentinelRef = useInfiniteScroll({
    hasNextPage: hasNextPage && !isFetchNextPageError,
    isFetchingNextPage,
    fetchNextPage,
  });

  if (isFetchNextPageError) {
    return (
      <div className="flex min-h-12 items-center justify-center py-6">
        <Button variant="secondary" size="sm" onClick={() => fetchNextPage()}>
          加载失败，点击重试
        </Button>
      </div>
    );
  }

  if (!hasNextPage) {
    return (
      <div className="flex min-h-12 items-center justify-center py-6 text-sm text-kumo-subtle">
        {endMessage}
      </div>
    );
  }

  // While the next page is prefetched, render only an invisible sentinel so no
  // loading animation is shown before the user reaches the bottom.
  return <div ref={sentinelRef} aria-hidden className="h-px" />;
}
