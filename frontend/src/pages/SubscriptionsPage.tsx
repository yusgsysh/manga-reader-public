import { useState } from "react";
import { BookmarkSimple } from "@phosphor-icons/react";
import { useWatched } from "../hooks/useGalleryList";
import { GalleryGrid } from "../components/gallery/GalleryGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { JumpSeekMenu } from "../components/common/JumpSeekMenu";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { PageHeader } from "../components/ui";
import type { ListingNavOptions } from "../types/gallery";

export function SubscriptionsPage() {
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
  } = useWatched(undefined, nav);

  const galleries = data?.pages.flatMap((page) => page.results) ?? [];

  return (
    <div>
      <PageHeader
        title="订阅"
        icon={<BookmarkSimple className="size-5" weight="fill" />}
        actions={
          <JumpSeekMenu
            nav={data?.pages[0]?.nav}
            value={nav}
            onChange={setNav}
          />
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
