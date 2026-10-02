import { useEffect, useState } from "react";
import { Skeleton } from "../ui";

function columnsForWidth(width: number): number {
  if (width < 480) return 2;
  if (width < 768) return 3;
  if (width < 1024) return 4;
  if (width < 1440) return 5;
  if (width < 1920) return 6;
  return 7;
}

function useGridColumnCount(): number {
  const [columns, setColumns] = useState(() =>
    columnsForWidth(window.innerWidth),
  );

  useEffect(() => {
    const onResize = () => setColumns(columnsForWidth(window.innerWidth));
    window.addEventListener("resize", onResize, { passive: true });
    return () => window.removeEventListener("resize", onResize);
  }, []);

  return columns;
}

function GallerySkeleton() {
  return (
    <div>
      <Skeleton className="aspect-[3/4] rounded-xl" />
      <div className="mt-2 space-y-1.5">
        <div className="min-h-10 space-y-1">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
        </div>
        <div className="flex items-center justify-between gap-2">
          <Skeleton className="h-5 w-16 rounded-full" />
          <Skeleton className="h-4 w-8" />
        </div>
        <div className="flex justify-end">
          <Skeleton className="h-4 w-16" />
        </div>
      </div>
    </div>
  );
}

export function GalleryGridSkeleton({ count }: { count?: number }) {
  const columns = useGridColumnCount();
  const items = count ?? columns * 2;

  return (
    <div className="gallery-grid">
      {Array.from({ length: items }).map((_, i) => (
        <GallerySkeleton key={i} />
      ))}
    </div>
  );
}
