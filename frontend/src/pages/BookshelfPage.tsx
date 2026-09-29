import { useBookshelf } from "../hooks/useBookshelf";
import { BookshelfGrid } from "../components/gallery/BookshelfGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";

export function BookshelfPage() {
  const {
    data,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useBookshelf();

  if (isLoading) {
    return <GalleryGridSkeleton />;
  }

  if (error) {
    return (
      <ErrorState
        message={error.message || "无法加载书架"}
        onRetry={() => refetch()}
      />
    );
  }

  const items = data?.pages.flatMap((page) => page.results) ?? [];

  if (items.length === 0) {
    return (
      <EmptyState
        message="书架还是空的"
        actionLabel="浏览首页"
        actionTo="/"
      />
    );
  }

  return (
    <div>
      <BookshelfGrid items={items} />
      <InfiniteScrollTrigger
        hasNextPage={!!hasNextPage}
        isFetchingNextPage={isFetchingNextPage}
        isFetchNextPageError={isFetchNextPageError}
        fetchNextPage={() => fetchNextPage()}
      />
    </div>
  );
}
