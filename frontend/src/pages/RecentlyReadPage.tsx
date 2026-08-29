import { useRecentlyRead } from "../hooks/useRecentlyRead";
import { BookshelfCard } from "../components/gallery/BookshelfCard";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { EmptyState } from "../components/common/EmptyState";
import { ErrorState } from "../components/common/ErrorState";

export function RecentlyReadPage() {
  const { data, isLoading, error, refetch } = useRecentlyRead();

  if (isLoading) {
    return <GalleryGridSkeleton count={8} />;
  }

  if (error) {
    return (
      <ErrorState message="加载最近阅读失败" onRetry={() => refetch()} />
    );
  }

  if (!data || data.results.length === 0) {
    return (
      <EmptyState
        message="还没有阅读记录"
        actionLabel="浏览首页"
        actionTo="/"
      />
    );
  }

  return (
    <div className="gallery-grid">
      {data.results.map((item) => (
        <BookshelfCard key={`${item.id}-${item.token}`} item={item} />
      ))}
    </div>
  );
}
