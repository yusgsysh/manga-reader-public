import { Badge } from "@cloudflare/kumo";
import type { GalleryListItem } from "../../types/gallery";
import { useNavigate } from "react-router";
import { thumbnailUrl } from "../../lib/image";
import { parseTagString } from "../../lib/tag";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { Tag as TagIcon } from "lucide-react";
import { useState, useRef, useEffect } from "react";

interface GalleryCardProps {
  gallery: GalleryListItem;
}

export function GalleryCard({ gallery }: GalleryCardProps) {
  const navigate = useNavigate();
  const [showTags, setShowTags] = useState(false);
  const tagsRef = useRef<HTMLDivElement>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const { translateTag, ready: tagDbReady } = useTagTranslation();

  const hasTags = (gallery.tags?.length ?? 0) > 0;

  const handleMouseEnter = () => {
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    setShowTags(true);
  };

  const handleMouseLeave = () => {
    timeoutRef.current = setTimeout(() => setShowTags(false), 200);
  };

  useEffect(() => {
    return () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
    };
  }, []);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (tagsRef.current && !tagsRef.current.contains(e.target as Node)) {
        setShowTags(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const formatTag = (raw: string) => {
    const tag = parseTagString(raw);
    if (tagDbReady) {
      return translateTag(tag);
    }
    return tag.namespace ? `${tag.namespace}:${tag.name}` : tag.name;
  };

  const [tagLeft, setTagLeft] = useState<string>("auto");

  const updateTagPosition = () => {
    if (tagsRef.current) {
      const rect = tagsRef.current.getBoundingClientRect();
      const tagWidth = 224;
      if (rect.left < tagWidth) {
        setTagLeft(`${-rect.left}px`);
      } else {
        setTagLeft("auto");
      }
    }
  };

  useEffect(() => {
    if (showTags) {
      updateTagPosition();
    }
  }, [showTags]);

  return (
    <div
      className="group cursor-pointer relative"
      onClick={() => navigate(`/gallery/${gallery.id}/${gallery.token}`)}
    >
      <div className="aspect-[3/4] overflow-hidden rounded-lg bg-kumo-recessed">
        <img
          src={thumbnailUrl(gallery.cover)}
          alt={gallery.title}
          loading="lazy"
          decoding="async"
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

      {hasTags && (
        <div
          ref={tagsRef}
          className="absolute bottom-2 right-2"
          onMouseEnter={handleMouseEnter}
          onMouseLeave={handleMouseLeave}
        >
          <button
            type="button"
            className="flex items-center justify-center size-7 rounded-full bg-black/50 text-white/80 hover:bg-black/70 hover:text-white transition-colors backdrop-blur-sm"
            onClick={(e) => {
              e.stopPropagation();
              setShowTags(!showTags);
            }}
          >
            <TagIcon className="size-3.5" />
          </button>

          {showTags && (
            <div
              className="absolute bottom-9 z-50 w-56 p-2 bg-kumo-elevated border border-kumo-border rounded-lg shadow-lg"
              style={{ left: tagLeft, right: tagLeft === "auto" ? 0 : "auto" }}
              onMouseEnter={handleMouseEnter}
              onMouseLeave={handleMouseLeave}
            >
              <div className="flex flex-wrap gap-1">
                {gallery.tags!.map((tag) => (
                  <span
                    key={tag}
                    className="inline-block px-1.5 py-0.5 text-[10px] bg-kumo-subtle rounded"
                  >
                    {formatTag(tag)}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

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

        {gallery.posted && (
          <p className="text-[10px] text-kumo-inactive">{gallery.posted}</p>
        )}
      </div>
    </div>
  );
}
