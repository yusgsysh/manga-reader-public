import { useState } from "react";
import { useGalleries } from "../hooks/useGalleryList";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { SimplePagination } from "../components/common/SimplePagination";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";

export function HomePage() {
  const [page, setPage] = useState(0);
  const { data, isLoading, error, refetch } = useGalleries(page);

  if (isLoading) {
    return <GalleryGridSkeleton />;
  }

  if (error) {
    return (
      <ErrorState
        message={error.message || "无法加载 Gallery"}
        onRetry={() => refetch()}
      />
    );
  }

  if (!data || data.results.length === 0) {
    return <EmptyState message="暂无 Gallery" />;
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
