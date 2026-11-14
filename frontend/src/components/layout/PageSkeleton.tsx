import { useLocation } from "react-router";
import { Skeleton } from "../ui";
import { GalleryGridSkeleton } from "../gallery/GallerySkeleton";

/*
 * First paint for a lazily loaded route, shown while the page's JS chunk is
 * still in flight.
 *
 * On a hard refresh the shell (header + main padding) is already mounted at
 * this point, so a centered full-viewport spinner threw that layout away and
 * made the boot read as a blank screen before the skeleton finally appeared.
 * These placeholders mirror the route's outer shape instead — the skeleton
 * cards land in the same grid the real galleries will occupy — so the chunk
 * swap moves nothing.
 *
 * Each entry only has to cover a route a refresh can land on; anything else
 * (settings, downloads, gallery detail) gets a neutral block placeholder.
 */

// Routes whose first screen is a PageHeader row above a gallery grid.
const LISTING_ROUTES = new Set([
  "/",
  "/watched",
  "/popular",
  "/bookshelf",
  "/recently-read",
]);

// Listing routes whose header always carries the filter / jump-seek buttons.
const ACTION_ROUTES = new Set(["/", "/watched"]);

export function PageSkeleton() {
  const { pathname } = useLocation();

  if (pathname === "/search") return <SearchSkeleton />;
  if (LISTING_ROUTES.has(pathname)) {
    return (
      <div>
        {ACTION_ROUTES.has(pathname) ? <HeaderActionsSkeleton /> : null}
        <GalleryGridSkeleton />
      </div>
    );
  }
  return <BlockSkeleton />;
}

// Mirrors PageHeader's action row: two size-8 icon buttons (AdvancedSearchMenu
// and JumpSeekMenu) with the same mb-2 / gap-2 metrics.
function HeaderActionsSkeleton() {
  return (
    <div className="mb-2 flex items-center justify-end gap-2">
      <Skeleton className="size-8 rounded-lg" />
      <Skeleton className="size-8 rounded-lg" />
    </div>
  );
}

// Search puts a sticky field + category card above its results, so a bare grid
// here would jump once the real page mounts.
function SearchSkeleton() {
  return (
    <div className="space-y-5">
      <div className="flex gap-2">
        <Skeleton className="h-9 flex-1" />
        <Skeleton className="h-9 w-24" />
      </div>
      <Skeleton className="h-[4.5rem] rounded-xl" />
      <GalleryGridSkeleton />
    </div>
  );
}

function BlockSkeleton() {
  return (
    <div className="mx-auto max-w-6xl space-y-4">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-24 rounded-xl" />
      <Skeleton className="h-24 rounded-xl" />
      <Skeleton className="h-24 rounded-xl" />
    </div>
  );
}
