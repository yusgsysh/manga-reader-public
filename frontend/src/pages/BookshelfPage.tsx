import { useState } from "react";
import { Books as BooksIcon } from "@phosphor-icons/react";
import { cn } from "@cloudflare/kumo";
import { useBookshelf } from "../hooks/useBookshelf";
import { BookshelfGrid } from "../components/gallery/BookshelfGrid";
import { GalleryGridSkeleton } from "../components/gallery/GallerySkeleton";
import { InfiniteScrollTrigger } from "../components/common/InfiniteScrollTrigger";
import { ErrorState } from "../components/common/ErrorState";
import { EmptyState } from "../components/common/EmptyState";
import { PageHeader } from "../components/ui";

type ShelfFilter = "all" | "reading" | "done";

const FILTERS: { value: ShelfFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "reading", label: "在读" },
  { value: "done", label: "已读完" },
];

export function BookshelfPage() {
  const [filter, setFilter] = useState<ShelfFilter>("all");
  const {
    data,
    isLoading,
    error,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = useBookshelf();

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

  const items = data?.pages.flatMap((page) => page.results) ?? [];
  const filtered = items.filter((item) => {
    if (filter === "all") return true;
    const reading = item.reading;
    if (!reading) return false;
    return filter === "done" ? reading.completed : !reading.completed;
  });

  const controls = (
    <div className="inline-flex items-center gap-0.5 rounded-lg bg-kumo-recessed p-0.5">
      {FILTERS.map((option) => (
        <button
          key={option.value}
          type="button"
          onClick={() => setFilter(option.value)}
          className={cn(
            "rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
            filter === option.value
              ? "bg-kumo-elevated text-kumo-default shadow-sm"
              : "text-kumo-subtle hover:text-kumo-default",
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );

  if (items.length === 0) {
    return (
      <div>
        <PageHeader
          title="书架"
          icon={<BooksIcon className="size-5" weight="fill" />}
        />
        <EmptyState
          message="书架还是空的"
          actionLabel="浏览首页"
          actionTo="/"
        />
      </div>
    );
  }

  return (
    <div>
      <PageHeader
        title="书架"
        icon={<BooksIcon className="size-5" weight="fill" />}
        actions={controls}
      />
      {filtered.length === 0 ? (
        <EmptyState message="没有符合条件的画廊" />
      ) : (
        <BookshelfGrid items={filtered} />
      )}
      <InfiniteScrollTrigger
        hasNextPage={!!hasNextPage}
        isFetchingNextPage={isFetchingNextPage}
        isFetchNextPageError={isFetchNextPageError}
        fetchNextPage={() => fetchNextPage()}
      />
    </div>
  );
}
