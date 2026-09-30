import { Button } from "@cloudflare/kumo";
import { CaretLeft, CaretRight } from "@phosphor-icons/react";

interface SimplePaginationProps {
  page: number;
  hasMore: boolean;
  totalPages?: number;
  onPageChange: (page: number) => void;
}

export function SimplePagination({
  page,
  hasMore,
  totalPages,
  onPageChange,
}: SimplePaginationProps) {
  if (totalPages !== undefined && totalPages <= 1) return null;
  if (page === 0 && !hasMore) return null;

  return (
    <div className="flex items-center justify-center gap-2 py-6">
      <Button
        variant="secondary"
        size="sm"
        disabled={page === 0}
        onClick={() => onPageChange(page - 1)}
      >
        <CaretLeft className="mr-1 size-4" weight="bold" />
        上一页
      </Button>
      <span className="px-4 text-sm text-kumo-subtle">
        {totalPages === undefined
          ? `第 ${page + 1} 页`
          : `${page + 1} / ${totalPages}`}
      </span>
      <Button
        variant="secondary"
        size="sm"
        disabled={!hasMore}
        onClick={() => onPageChange(page + 1)}
      >
        下一页
        <CaretRight className="ml-1 size-4" weight="bold" />
      </Button>
    </div>
  );
}
