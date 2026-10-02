import type { GalleryCategory } from "../../types/gallery";
import { Link } from "react-router";
import { thumbnailUrl } from "../../lib/image";
import { parseTagString } from "../../lib/tag";
import { formatPosted } from "../../lib/time";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { Star, Tag as TagIcon } from "@phosphor-icons/react";
import { createPortal } from "react-dom";
import { useState, useRef, useEffect } from "react";
import { CategoryChip } from "./CategoryChip";

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

export interface GalleryCardData {
  id: number;
  token: string;
  title: string;
  category?: GalleryCategory;
  image?: string;
  rating?: number;
  pages?: number;
  posted?: string;
  tags?: string[];
  reading?: {
    progress: number;
    completed: boolean;
  };
}

interface GalleryCardProps {
  gallery: GalleryCardData;
}

export function GalleryCard({ gallery }: GalleryCardProps) {
  const [showTags, setShowTags] = useState(false);
  const [loadedImage, setLoadedImage] = useState<string | null>(null);
  const [popoverPos, setPopoverPos] = useState<{
    left: number;
    bottom: number;
  } | null>(null);
  const tagsRef = useRef<HTMLDivElement>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const { translateTag, ready: tagDbReady } = useTagTranslation();

  const hasTags = (gallery.tags?.length ?? 0) > 0;
  const galleryHref = `/gallery/${gallery.id}/${gallery.token}`;
  const image = gallery.image ? thumbnailUrl(gallery.image) : "";
  const imageLoaded = image !== "" && loadedImage === image;

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

  const progress = gallery.reading
    ? Math.round(gallery.reading.progress * 100)
    : 0;

  return (
    <div className="gallery-card group relative">
      <div className="relative">
        <Link to={galleryHref} aria-label={gallery.title} className="block">
          <div className="relative aspect-[3/4] overflow-hidden rounded-xl bg-kumo-recessed ring-1 ring-kumo-hairline/70 transition-all duration-200 group-hover:-translate-y-0.5 group-hover:shadow-lg">
            {image ? (
              <img
                src={image}
                alt={gallery.title}
                loading="lazy"
                decoding="async"
                className="app-image h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
                onLoad={() => setLoadedImage(image)}
                onError={(e) => {
                  setLoadedImage(image);
                  e.currentTarget.style.display = "none";
                  e.currentTarget.nextElementSibling?.classList.remove("hidden");
                }}
              />
            ) : null}
            <div
              className={`${
                image ? "hidden" : "flex"
              } h-full items-center justify-center p-4 text-center text-xs text-kumo-subtle`}
            >
              图片不可用
            </div>

            {/* image loading placeholder */}
            {image && !imageLoaded && (
              <div className="absolute inset-0 overflow-hidden">
                <div className="skeleton-shimmer h-full w-full bg-kumo-recessed" />
              </div>
            )}

            {/* progress bar */}
            {gallery.reading && (
              <div className="absolute inset-x-0 bottom-0 h-1 bg-black/30">
                <div
                  className="h-full bg-[var(--app-accent)]"
                  style={{ width: `${progress}%` }}
                />
              </div>
            )}
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
              className="flex size-7 items-center justify-center rounded-full bg-white/15 text-white ring-1 ring-white/25 backdrop-blur-md transition-colors hover:bg-white/25"
              onClick={toggleTags}
              aria-label="查看标签"
              aria-expanded={showTags}
            >
              <TagIcon className="size-3.5" weight="fill" />
            </button>
          </div>
        )}

        {gallery.reading && (
          <div className="pointer-events-none absolute left-2 top-2">
            <span className="rounded-full bg-black/55 px-2 py-0.5 text-[11px] font-medium text-white/90 backdrop-blur-sm">
              {gallery.reading.completed ? "已读完" : `已读 ${progress}%`}
            </span>
          </div>
        )}

        {hasTags &&
          showTags &&
          popoverPos &&
          createPortal(
            <div
              className="fixed z-50 w-56 rounded-xl border border-kumo-hairline bg-kumo-elevated p-2 shadow-lg"
              style={{ left: popoverPos.left, bottom: popoverPos.bottom }}
              onMouseEnter={handleMouseEnter}
              onMouseLeave={handleMouseLeave}
            >
              <div className="flex flex-wrap gap-1">
                {gallery.tags!.map((tag) => (
                  <span
                    key={tag}
                    className="inline-block rounded-md bg-kumo-recessed px-1.5 py-0.5 text-[11px]"
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
          <h3 className="line-clamp-2 min-h-10 text-sm font-medium leading-5">
            {gallery.title}
          </h3>

          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-kumo-subtle">
            {gallery.category && <CategoryChip category={gallery.category} />}
            {typeof gallery.rating === "number" && (
              <span className="inline-flex items-center gap-0.5">
                <Star className="size-3.5 text-kumo-warning" weight="fill" />
                {gallery.rating.toFixed(1)}
              </span>
            )}
            {typeof gallery.pages === "number" && gallery.pages > 0 && (
              <span className="tnum">{gallery.pages}p</span>
            )}
            {gallery.posted && (
              <span className="text-kumo-inactive">
                {formatPosted(gallery.posted)}
              </span>
            )}
          </div>
        </div>
      </Link>
    </div>
  );
}
