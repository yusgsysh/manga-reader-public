import { useState } from "react";
import { useBookshelf } from "../hooks/useBookshelf";
import { BookshelfGrid } from "../components/gallery/BookshelfGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { SimplePagination } from "../components/common/SimplePagination";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";

export function BookshelfPage() {
  const [page, setPage] = useState(0);
  const { data, isLoading, error, refetch } = useBookshelf(page);

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

  if (!data || data.results.length === 0) {
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
      <BookshelfGrid items={data.results} />
      <SimplePagination
        page={data.page}
        hasMore={(data.page + 1) * data.page_size < data.total}
        onPageChange={setPage}
      />
    </div>
  );
}
