import { Fire } from "@phosphor-icons/react";
import { usePopular } from "../hooks/useGalleryList";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { PageHeader } from "../components/ui";

export function PopularPage() {
  const {
    data,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = usePopular();

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

  const galleries = data?.pages.flatMap((page) => page.results) ?? [];

  if (galleries.length === 0) {
    return (
      <div>
        <PageHeader
          title="热门"
          description="近期最受欢迎的画廊"
          icon={<Fire className="size-5" weight="fill" />}
        />
        <EmptyState message="暂无热门 Gallery" />
      </div>
    );
  }

  return (
    <div>
      <PageHeader
        title="热门"
        description="近期最受欢迎的画廊"
        icon={<Fire className="size-5" weight="fill" />}
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
