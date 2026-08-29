import { Badge } from "@cloudflare/kumo";
import type { GalleryListItem } from "../../types/gallery";
import { GalleryTags } from "./GalleryTags";
import { useNavigate } from "react-router";
import { thumbnailUrl } from "../../lib/image";

interface GalleryCardProps {
  gallery: GalleryListItem;
}

export function GalleryCard({ gallery }: GalleryCardProps) {
  const navigate = useNavigate();

  return (
    <div
      className="group cursor-pointer"
      onClick={() => navigate(`/gallery/${gallery.id}/${gallery.token}`)}
    >
      <div className="aspect-[3/4] overflow-hidden rounded-lg bg-kumo-recessed">
        <img
          src={thumbnailUrl(gallery.cover)}
          alt={gallery.title}
          loading="lazy"
          className="h-full w-full object-cover transition-transform duration-200 group-hover:scale-105"
          onError={(e) => {
            e.currentTarget.style.display = "none";
            e.currentTarget.nextElementSibling?.classList.remove("hidden");
          }}
        />
        <div className="hidden flex h-full items-center justify-center p-4 text-center text-xs text-kumo-subtle">
          Image unavailable
        </div>
      </div>

      <div className="mt-2 space-y-1">
        <h3 className="line-clamp-2 text-sm font-medium leading-tight">
          {gallery.title}
        </h3>

        <div className="flex items-center gap-2 text-xs text-kumo-subtle">
          <Badge variant="secondary" className="text-[10px]">
            {gallery.category}
          </Badge>
          <span>★ {gallery.rating.toFixed(1)}</span>
          <span>{gallery.pages}p</span>
        </div>

        {(gallery.tags?.length ?? 0) > 0 && (
          <div className="pt-0.5">
            <GalleryTags tags={gallery.tags ?? []} max={3} />
          </div>
        )}

        {gallery.posted && (
          <p className="text-[10px] text-kumo-inactive">{gallery.posted}</p>
        )}
      </div>
    </div>
  );
}
