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

export function GalleryGridSkeleton({ count = 12 }: { count?: number }) {
  return (
    <div className="gallery-grid">
      {Array.from({ length: count }).map((_, i) => (
        <GallerySkeleton key={i} />
      ))}
    </div>
  );
}
