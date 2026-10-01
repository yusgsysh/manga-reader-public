import { BookmarkSimple } from "@phosphor-icons/react";
import { useWatched } from "../hooks/useGalleryList";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { PageHeader } from "../components/ui";

export function SubscriptionsPage() {
  const {
    data,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useWatched();

  if (isLoading) {
    return <GalleryGridSkeleton />;
  }

  if (error) {
    return (
      <ErrorState
        message={error.message || "无法加载订阅内容"}
        onRetry={() => refetch()}
      />
    );
  }

  const galleries = data?.pages.flatMap((page) => page.results) ?? [];

  if (galleries.length === 0) {
    return (
      <div>
        <PageHeader
          title="订阅"
          icon={<BookmarkSimple className="size-5" weight="fill" />}
        />
        <EmptyState
          message="暂无订阅内容"
          actionLabel="浏览首页"
          actionTo="/"
        />
      </div>
    );
  }

  return (
    <div>
      <PageHeader
        title="订阅"
        icon={<BookmarkSimple className="size-5" weight="fill" />}
      />
      <GalleryGrid galleries={galleries} />
      <InfiniteScrollTrigger
        hasNextPage={!!hasNextPage}
        isFetchingNextPage={isFetchingNextPage}
        isFetchNextPageError={isFetchNextPageError}
        fetchNextPage={() => fetchNextPage()}
      />
    </div>
  );
}
