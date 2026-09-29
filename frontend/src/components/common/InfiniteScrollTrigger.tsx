import { Button } from "@cloudflare/kumo";
import { Loader2 } from "lucide-react";
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

  return (
    <div
      ref={sentinelRef}
      className="flex min-h-12 items-center justify-center py-6 text-sm text-kumo-subtle"
    >
      {isFetchingNextPage ? (
        <span className="flex items-center gap-2">
          <Loader2 className="size-4 animate-spin" />
          加载中...
        </span>
      ) : isFetchNextPageError ? (
        <Button variant="secondary" size="sm" onClick={() => fetchNextPage()}>
          加载失败，点击重试
        </Button>
      ) : hasNextPage ? (
        <span>向下滚动加载更多</span>
      ) : (
        <span>{endMessage}</span>
      )}
    </div>
  );
}
