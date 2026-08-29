import { useState } from "react";
import { useWatched } from "../hooks/useGalleryList";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { SimplePagination } from "../components/common/SimplePagination";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";

export function SubscriptionsPage() {
  const [page, setPage] = useState(0);
  const { data, isLoading, error, refetch } = useWatched(page);

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

  if (!data || data.results.length === 0) {
    return (
      <EmptyState
        message="暂无订阅内容"
        actionLabel="浏览首页"
        actionTo="/"
      />
    );
  }

  const hasMore = data.results.length >= data.page_size;

  return (
    <div>
      <GalleryGrid galleries={data.results} />
      <SimplePagination
        page={data.page}
        hasMore={hasMore}
        onPageChange={setPage}
      />
    </div>
  );
}
