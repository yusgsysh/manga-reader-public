import { usePopular } from "../hooks/useGalleryList";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";

export function PopularPage() {
  const { data, isLoading, error, refetch } = usePopular(0);

  if (isLoading) {
    return <GalleryGridSkeleton />;
  }

  if (error) {
    return (
      <ErrorState
        message={error.message || "无法加载热门内容"}
        onRetry={() => refetch()}
      />
    );
  }

  if (!data || data.results.length === 0) {
    return <EmptyState message="暂无热门 Gallery" />;
  }

  return (
    <div>
      <GalleryGrid galleries={data.results} />
    </div>
  );
}
