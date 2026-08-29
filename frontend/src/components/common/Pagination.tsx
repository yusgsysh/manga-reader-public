import { Pagination as KumoPagination } from "@cloudflare/kumo";

interface PaginationProps {
  page: number;
  pageSize: number;
  total: number;
  onPageChange: (page: number) => void;
}

export function Pagination({
  page,
  pageSize,
  total,
  onPageChange,
}: PaginationProps) {
  const totalPages = Math.ceil(total / pageSize);
  if (totalPages <= 1) return null;

  return (
    <div className="flex justify-center py-6">
      <KumoPagination
        page={page + 1}
        setPage={(p) => onPageChange(p - 1)}
        perPage={pageSize}
        totalCount={total}
        controls="simple"
      />
    </div>
  );
}
