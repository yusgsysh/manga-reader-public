interface GalleryTagsProps {
  tags: string[];
  max?: number;
}

export function GalleryTags({ tags, max = 3 }: GalleryTagsProps) {
  const safeTags = tags ?? [];
  const shown = safeTags.slice(0, max);
  const remaining = safeTags.length - max;

  return (
    <div className="flex flex-wrap gap-1">
      {shown.map((tag) => (
        <span
          key={tag}
          className="inline-block rounded bg-kumo-recessed px-1.5 py-0.5 text-[10px] text-kumo-subtle"
        >
          {tag}
        </span>
      ))}
      {remaining > 0 && (
        <span className="inline-block rounded bg-kumo-recessed px-1.5 py-0.5 text-[10px] text-kumo-inactive">
          +{remaining}
        </span>
      )}
    </div>
  );
}
