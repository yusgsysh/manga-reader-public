import { Badge } from "@cloudflare/kumo";
import type { BookshelfItem } from "../../types/gallery";
import { useNavigate } from "react-router";

interface BookshelfCardProps {
  item: BookshelfItem;
}

export function BookshelfCard({ item }: BookshelfCardProps) {
  const navigate = useNavigate();

  return (
    <div
      className="group cursor-pointer"
      onClick={() => navigate(`/gallery/${item.id}/${item.token}`)}
    >
      <div className="aspect-[3/4] overflow-hidden rounded-lg bg-kumo-recessed">
        <img
          src={item.thumbnail}
          alt={item.title}
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
          {item.title}
        </h3>

        <div className="flex items-center gap-2 text-xs text-kumo-subtle">
          <Badge variant="secondary" className="text-[10px]">
            {item.category}
          </Badge>
          <span>{item.pages}p</span>
        </div>

        {item.reading && (
          <div className="flex items-center gap-1 text-[10px] text-kumo-inactive">
            <span>
              {item.reading.completed
                ? "已读完"
                : `已读 ${Math.round(item.reading.progress * 100)}%`}
            </span>
          </div>
        )}
      </div>
    </div>
  );
}
