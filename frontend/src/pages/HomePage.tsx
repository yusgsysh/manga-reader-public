import { useState } from "react";
import { Link } from "react-router";
import { Sparkle } from "@phosphor-icons/react";
import { useGalleries } from "../hooks/useGalleryList";
import { useRecentlyRead } from "../hooks/useRecentlyRead";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { GalleryCard } from "../components/gallery/GalleryCard";
import { readingToCard } from "../components/gallery/mappers";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { JumpSeekPanel } from "../components/common/JumpSeekPanel";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { ListingLayout } from "../components/layout/ListingLayout";
import { PageHeader, Section } from "../components/ui";
import type { ListingNavOptions } from "../types/gallery";

export function HomePage() {
  const [nav, setNav] = useState<ListingNavOptions>({});
  const {
    data,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useGalleries(undefined, nav);
  const { items: recent } = useRecentlyRead();

  const galleries = data?.pages.flatMap((page) => page.results) ?? [];
  const continueItems = recent.slice(0, 12);

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
        <ListingLayout
          sidebar={
            <JumpSeekPanel
              nav={data?.pages[0]?.nav}
              value={nav}
              onChange={setNav}
            />
          }
        >
          {isLoading ? (
            <GalleryGridSkeleton />
          ) : error ? (
            <ErrorState
              message={error.message || "无法加载 Gallery"}
              onRetry={() => refetch()}
            />
          ) : galleries.length === 0 ? (
            <EmptyState message="暂无 Gallery" />
          ) : (
            <>
              <GalleryGrid galleries={galleries} />
              <InfiniteScrollTrigger
                hasNextPage={!!hasNextPage}
                isFetchingNextPage={isFetchingNextPage}
                isFetchNextPageError={isFetchNextPageError}
                fetchNextPage={() => fetchNextPage()}
              />
            </>
          )}
        </ListingLayout>
      </div>
    </div>
  );
}
