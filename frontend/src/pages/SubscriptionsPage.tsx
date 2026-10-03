import { useSearchParams } from "react-router";
import { useMemo } from "react";
import { BookmarkSimple } from "@phosphor-icons/react";
import { useWatched } from "../hooks/useGalleryList";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { JumpSeekMenu } from "../components/common/JumpSeekMenu";
import { AdvancedSearchMenu } from "../components/search/AdvancedSearchMenu";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { PageHeader } from "../components/ui";
import {
  parseListFilters,
  parseNavOptions,
  withListFilters,
  withNavOptions,
} from "../lib/listingParams";
import type { GalleryListFilters, ListingNavOptions } from "../types/gallery";
import { useLastListRoute } from "../hooks/useLastListRoute";

export function SubscriptionsPage() {
  useLastListRoute("/watched");
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
  } = useWatched(filters, nav);

  const galleries = useMemo(
    () => data?.pages.flatMap((page) => page.results) ?? [],
    [data],
  );

  const handleFiltersChange = (next: GalleryListFilters) => {
    setSearchParams(withListFilters(searchParams, next));
  };

  const handleNavChange = (next: ListingNavOptions) => {
    setSearchParams(withNavOptions(searchParams, next));
  };

  return (
    <div>
      <PageHeader
        title="订阅"
        icon={<BookmarkSimple className="size-5" weight="fill" />}
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
          message={error.message || "无法加载订阅内容"}
          onRetry={() => refetch()}
        />
      ) : galleries.length === 0 ? (
        <EmptyState
          message="暂无订阅内容"
          actionLabel="浏览首页"
          actionTo="/"
        />
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
  );
}
