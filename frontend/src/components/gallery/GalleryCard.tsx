import { Badge } from "@cloudflare/kumo";
import type { GalleryListItem } from "../../types/gallery";
import { Link } from "react-router";
import { thumbnailUrl } from "../../lib/image";
import { parseTagString } from "../../lib/tag";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { Tag as TagIcon } from "lucide-react";
import { createPortal } from "react-dom";
import { useState, useRef, useEffect } from "react";

const TAG_POPOVER_WIDTH = 224;
const VIEWPORT_MARGIN = 8;

function computePopoverPosition(element: HTMLElement) {
  const rect = element.getBoundingClientRect();
  const maxLeft = window.innerWidth - TAG_POPOVER_WIDTH - VIEWPORT_MARGIN;
  return {
    left: Math.max(
      VIEWPORT_MARGIN,
      Math.min(rect.right - TAG_POPOVER_WIDTH, maxLeft),
    ),
    bottom: window.innerHeight - rect.top + VIEWPORT_MARGIN,
  };
}

interface GalleryCardProps {
  gallery: GalleryListItem;
}

export function GalleryCard({ gallery }: GalleryCardProps) {
  const [showTags, setShowTags] = useState(false);
  const [popoverPos, setPopoverPos] = useState<{
    left: number;
    bottom: number;
  } | null>(null);
  const tagsRef = useRef<HTMLDivElement>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const { translateTag, ready: tagDbReady } = useTagTranslation();

  const hasTags = (gallery.tags?.length ?? 0) > 0;
  const galleryHref = `/gallery/${gallery.id}/${gallery.token}`;

  const openTags = () => {
    const el = tagsRef.current;
    if (el) setPopoverPos(computePopoverPosition(el));
    setShowTags(true);
  };

  const handleMouseEnter = () => {
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    openTags();
  };

  const handleMouseLeave = () => {
    timeoutRef.current = setTimeout(() => setShowTags(false), 200);
  };

  const toggleTags = () => {
    if (showTags) setShowTags(false);
    else openTags();
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

  useEffect(() => {
    if (!showTags) return;
    const close = () => setShowTags(false);
    const reposition = () => {
      const el = tagsRef.current;
      if (el) setPopoverPos(computePopoverPosition(el));
    };
    window.addEventListener("scroll", close, { passive: true });
    window.addEventListener("resize", reposition);
    return () => {
      window.removeEventListener("scroll", close);
      window.removeEventListener("resize", reposition);
    };
  }, [showTags]);

  const formatTag = (raw: string) => {
    const tag = parseTagString(raw);
    if (tagDbReady) {
      return translateTag(tag);
    }
    return tag.namespace ? `${tag.namespace}:${tag.name}` : tag.name;
  };

  return (
    <div className="gallery-card group relative">
      <div className="relative">
        <Link to={galleryHref} aria-label={gallery.title} className="block">
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
        </Link>

        {hasTags && (
          <div
            ref={tagsRef}
            className="absolute bottom-2 right-2 z-10"
            onMouseEnter={handleMouseEnter}
            onMouseLeave={handleMouseLeave}
          >
            <button
              type="button"
              className="flex size-7 items-center justify-center rounded-full bg-black/50 text-white/80 backdrop-blur-sm transition-colors hover:bg-black/70 hover:text-white"
              onClick={toggleTags}
              aria-label="查看标签"
              aria-expanded={showTags}
            >
              <TagIcon className="size-3.5" />
            </button>
          </div>
        )}

        {hasTags &&
          showTags &&
          popoverPos &&
          createPortal(
            <div
              className="fixed z-50 w-56 rounded-lg border border-kumo-border bg-kumo-elevated p-2 shadow-lg"
              style={{ left: popoverPos.left, bottom: popoverPos.bottom }}
              onMouseEnter={handleMouseEnter}
              onMouseLeave={handleMouseLeave}
            >
              <div className="flex flex-wrap gap-1">
                {gallery.tags!.map((tag) => (
                  <span
                    key={tag}
                    className="inline-block rounded bg-kumo-subtle px-1.5 py-0.5 text-[10px]"
                  >
                    {formatTag(tag)}
                  </span>
                ))}
              </div>
            </div>,
            document.body,
          )}
      </div>

      <Link to={galleryHref} className="block">
        <div className="mt-2 space-y-1">
          <h3 className="line-clamp-2 text-sm font-medium leading-tight">
            {gallery.title}
          </h3>

          <div className="flex flex-wrap items-center gap-2 text-xs text-kumo-subtle">
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
      </Link>
    </div>
  );
}
