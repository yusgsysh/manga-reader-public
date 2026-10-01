import { useState } from "react";
import { Link } from "react-router";
import { pageThumbnailUrl } from "../../lib/image";
import { SimplePagination } from "../common/SimplePagination";
import type { GalleryPage } from "../../types/reader";

const PER_PAGE = 20;

interface PageThumbnailGridProps {
  id: number;
  token: string;
  pages: GalleryPage[];
}

export function PageThumbnailGrid({ id, token, pages }: PageThumbnailGridProps) {
  const [gridPage, setGridPage] = useState(0);

  const totalPages = Math.max(1, Math.ceil(pages.length / PER_PAGE));
  const current = Math.min(gridPage, totalPages - 1);
  const slice = pages.slice(current * PER_PAGE, current * PER_PAGE + PER_PAGE);

  return (
    <div className="space-y-2">
      <div className="page-thumb-grid">
        {slice.map((page) => (
          <Link
            key={page.index}
            to={`/reader/${id}/${token}?page=${page.index}`}
            className="page-thumb-item"
            aria-label={`第 ${page.index + 1} 页`}
          >
            {page.thumbnail ? (
              <img
                src={pageThumbnailUrl(page.thumbnail)}
                alt={`第 ${page.index + 1} 页`}
                loading="lazy"
                decoding="async"
              />
            ) : (
              <span className="page-thumb-placeholder" aria-hidden />
            )}
            <span className="page-thumb-number">{page.index + 1}</span>
          </Link>
        ))}
      </div>
      <SimplePagination
        page={current}
        hasMore={current < totalPages - 1}
        totalPages={totalPages}
        onPageChange={setGridPage}
      />
    </div>
  );
}
