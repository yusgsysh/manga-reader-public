import { Link } from "react-router";
import { Sparkle } from "@phosphor-icons/react";
import { useGalleries } from "../hooks/useGalleryList";
import { useRecentlyRead } from "../hooks/useRecentlyRead";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { GalleryCard } from "../components/gallery/GalleryCard";
import { readingToCard } from "../components/gallery/mappers";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { PageHeader, Section } from "../components/ui";

export function HomePage() {
  const {
    data,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useGalleries();
  const { items: recent } = useRecentlyRead();

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

  const galleries = data?.pages.flatMap((page) => page.results) ?? [];
  const continueItems = recent.slice(0, 12);

  if (galleries.length === 0 && continueItems.length === 0) {
    return <EmptyState message="暂无 Gallery" />;
  }

  return (
    <div className="space-y-8">
      {continueItems.length > 0 && (
        <Section
          title="继续阅读"
          action={
            <Link
              to="/recently-read"
              className="text-xs font-medium text-[var(--app-accent)] hover:underline"
            >
              查看全部
            </Link>
          }
        >
          <div className="-mx-1 flex gap-4 overflow-x-auto px-1 pb-2">
            {continueItems.map((item) => (
              <div
                key={`${item.id}-${item.token}`}
                className="w-32 shrink-0 sm:w-36"
              >
                <GalleryCard gallery={readingToCard(item)} />
              </div>
            ))}
          </div>
        </Section>
      )}

      <div>
        <PageHeader
          title="最新"
          icon={<Sparkle className="size-5" weight="fill" />}
        />
        <GalleryGrid galleries={galleries} />
        <InfiniteScrollTrigger
          hasNextPage={!!hasNextPage}
          isFetchingNextPage={isFetchingNextPage}
          isFetchNextPageError={isFetchNextPageError}
          fetchNextPage={() => fetchNextPage()}
        />
      </div>
    </div>
  );
}
