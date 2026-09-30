import { useEffect, useRef, useState } from "react";
import { Button, Dialog } from "@cloudflare/kumo";
import { Trash, CircleNotch } from "@phosphor-icons/react";
import { useRecentlyRead, useCleanupReadingProgress } from "../hooks/useRecentlyRead";
import { BookshelfCard } from "../components/gallery/BookshelfCard";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { EmptyState } from "../components/common/EmptyState";
import { ErrorState } from "../components/common/ErrorState";

export function RecentlyReadPage() {
  const {
    items,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useRecentlyRead();
  const cleanup = useCleanupReadingProgress();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const cleanupFiredRef = useRef(false);

  // 访问页面时清理 30 天以前的阅读记录（仅触发一次）。
  useEffect(() => {
    if (cleanupFiredRef.current) return;
    cleanupFiredRef.current = true;
    cleanup.mutate(30);
  }, [cleanup]);

  if (isLoading) {
    return <GalleryGridSkeleton count={8} />;
  }

  if (error) {
    return (
      <ErrorState message="加载最近阅读失败" onRetry={() => refetch()} />
    );
  }

  return (
    <div>
      {items.length > 0 && (
        <div className="mb-4 flex items-center justify-end">
          <Button
            variant="ghost"
            size="sm"
            className="text-kumo-danger"
            onClick={() => setConfirmOpen(true)}
          >
            <Trash className="mr-1 size-4" weight="bold" />
            清理记录
          </Button>
        </div>
      )}

      {items.length === 0 ? (
        <EmptyState
          message="还没有阅读记录"
          actionLabel="浏览首页"
          actionTo="/"
        />
      ) : (
        <div className="gallery-grid">
          {items.map((item) => (
            <BookshelfCard key={`${item.id}-${item.token}`} item={item} />
          ))}
        </div>
      )}

      {items.length > 0 && (
        <InfiniteScrollTrigger
          hasNextPage={!!hasNextPage}
          isFetchingNextPage={isFetchingNextPage}
          isFetchNextPageError={isFetchNextPageError}
          fetchNextPage={() => fetchNextPage()}
        />
      )}

      <Dialog.Root open={confirmOpen} onOpenChange={setConfirmOpen}>
        <Dialog className="p-6">
          <Dialog.Title className="text-base font-semibold">
            删除所有阅读记录
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            确定要删除全部阅读记录吗？此操作无法撤销。
          </Dialog.Description>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="secondary">取消</Button>} />
            <Button
              variant="destructive"
              onClick={() => {
                cleanup.mutate(0);
                setConfirmOpen(false);
              }}
              disabled={cleanup.isPending}
            >
              {cleanup.isPending && (
                <CircleNotch className="mr-1 size-4 animate-spin" />
              )}
              删除
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}
