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

function PageThumbnail({
  id,
  token,
  page,
}: {
  id: number;
  token: string;
  page: GalleryPage;
}) {
  const [loaded, setLoaded] = useState(false);
  const src = page.thumbnail ? pageThumbnailUrl(page.thumbnail) : "";
  const showShimmer = src !== "" && !loaded;

  return (
    <Link
      to={`/reader/${id}/${token}?page=${page.index}`}
      className="page-thumb-item"
      aria-label={`第 ${page.index + 1} 页`}
    >
      {src ? (
        <img
          src={src}
          alt={`第 ${page.index + 1} 页`}
          loading="lazy"
          decoding="async"
          className="app-image"
          onLoad={() => setLoaded(true)}
          onError={() => setLoaded(true)}
        />
      ) : (
        <span className="page-thumb-placeholder" aria-hidden />
      )}
      {showShimmer && (
        <span
          className="skeleton-shimmer absolute inset-0 bg-kumo-recessed"
          aria-hidden
        />
      )}
      <span className="page-thumb-number">{page.index + 1}</span>
    </Link>
  );
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
          <PageThumbnail key={page.index} id={id} token={token} page={page} />
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
