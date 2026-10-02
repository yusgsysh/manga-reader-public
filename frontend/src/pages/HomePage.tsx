import { Link, useSearchParams } from "react-router";
import { Sparkle } from "@phosphor-icons/react";
import { useGalleries } from "../hooks/useGalleryList";
import { useRecentlyRead } from "../hooks/useRecentlyRead";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { GalleryCard } from "../components/gallery/GalleryCard";
import { readingToCard } from "../components/gallery/mappers";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { JumpSeekMenu } from "../components/common/JumpSeekMenu";
import { AdvancedSearchMenu } from "../components/search/AdvancedSearchMenu";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { PageHeader, Section } from "../components/ui";
import {
  parseListFilters,
  parseNavOptions,
  withListFilters,
  withNavOptions,
} from "../lib/listingParams";
import type { GalleryListFilters, ListingNavOptions } from "../types/gallery";

export function HomePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const nav = parseNavOptions(searchParams);
  const filters = parseListFilters(searchParams);
  const {
    data,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useGalleries(filters, nav);
  const { items: recent } = useRecentlyRead();

  const galleries = data?.pages.flatMap((page) => page.results) ?? [];
  const continueItems = recent.slice(0, 12);

  const handleFiltersChange = (next: GalleryListFilters) => {
    setSearchParams(withListFilters(searchParams, next));
  };

  const handleNavChange = (next: ListingNavOptions) => {
    setSearchParams(withNavOptions(searchParams, next));
  };

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
          actions={
            <>
              <AdvancedSearchMenu value={filters} onApply={handleFiltersChange} />
              <JumpSeekMenu
                nav={data?.pages[0]?.nav}
                value={nav}
                onChange={handleNavChange}
              />
            </>
          }
        />
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
      </div>
    </div>
  );
}
