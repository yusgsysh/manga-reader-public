import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { pageThumbnailUrl } from "../../lib/image";
import { loadThumbnails, THUMBNAIL_CONCURRENCY } from "../../lib/thumbnails";
import { SimplePagination } from "../common/SimplePagination";

const PER_PAGE = 20;

interface PageThumbnailGridProps {
  id: number;
  token: string;
  total: number;
}

function PageThumbnail({
  id,
  token,
  index,
}: {
  id: number;
  token: string;
  index: number;
}) {
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  // Index-addressed: the backend resolves the sprite geometry itself, so the
  // grid never needs the full page list.
  const src = token ? pageThumbnailUrl(id, token, index) : "";
  const showShimmer = src !== "" && !loaded && !failed;

  return (
    <Link
      to={`/reader/${id}/${token}?page=${index}`}
      className="page-thumb-item"
      aria-label={`第 ${index + 1} 页`}
    >
      {src && !failed ? (
        <img
          src={src}
          alt={`第 ${index + 1} 页`}
          decoding="async"
          className="app-image"
          onLoad={() => setLoaded(true)}
          onError={() => {
            setFailed(true);
            setLoaded(true);
          }}
        />
      ) : (
        <span className="page-thumb-placeholder" aria-hidden />
      )}
      {showShimmer && (
        // Same structure as the homepage card: the wrapper is absolute while
        // .skeleton-shimmer keeps `position: relative`, so the shimmer fills
        // the tile instead of collapsing to nothing.
        <div className="absolute inset-0 overflow-hidden" aria-hidden>
          <div className="skeleton-shimmer h-full w-full bg-kumo-recessed" />
        </div>
      )}
      <span className="page-thumb-number">{index + 1}</span>
    </Link>
  );
}

export function PageThumbnailGrid({
  id,
  token,
  total,
}: PageThumbnailGridProps) {
  const [gridPage, setGridPage] = useState(0);
  const gridRef = useRef<HTMLDivElement>(null);

  // Only the current grid page's indices are rendered/requested, derived from
  // the gallery's page count; there is no full page-list fetch here.
  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));
  const current = Math.min(gridPage, totalPages - 1);
  const start = current * PER_PAGE;
  const end = Math.min(total, start + PER_PAGE);
  const indices = Array.from(
    { length: Math.max(0, end - start) },
    (_, i) => start + i,
  );

  // Feed the page's index-addressed thumbnails through the shared bounded
  // queue so a page of 20 tiles never fires 20 upstream resolves at once.
  useEffect(() => {
    const el = gridRef.current;
    if (!el) return;
    return loadThumbnails(el, { concurrency: THUMBNAIL_CONCURRENCY });
  }, []);

  return (
    <div className="space-y-2">
      <div ref={gridRef} className="page-thumb-grid">
        {indices.map((index) => (
          <PageThumbnail key={index} id={id} token={token} index={index} />
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
