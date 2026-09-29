import { useEffect, useState } from "react";

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

export function GallerySkeleton() {
  return (
    <div className="animate-pulse">
      <div className="aspect-[3/4] rounded-lg bg-kumo-recessed" />
      <div className="mt-2 space-y-2">
        <div className="h-4 w-3/4 rounded bg-kumo-recessed" />
        <div className="h-3 w-1/2 rounded bg-kumo-recessed" />
        <div className="h-3 w-1/3 rounded bg-kumo-recessed" />
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
