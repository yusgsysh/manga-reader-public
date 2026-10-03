import type { GalleryCategory } from "../../types/gallery";
import { Link } from "react-router";
import { thumbnailUrl } from "../../lib/image";
import { parseTagString } from "../../lib/tag";
import { languageCodeFromTags } from "../../lib/language";
import { formatPosted } from "../../lib/time";
import { useTagTranslation } from "../../hooks/useTagTranslation";
import { Star, Tag as TagIcon } from "@phosphor-icons/react";
import { createPortal } from "react-dom";
import { memo, useState, useRef, useEffect, useMemo } from "react";
import { CategoryChip } from "./CategoryChip";

const TAG_POPOVER_WIDTH = 224;
const VIEWPORT_MARGIN = 8;

// Namespaces hidden from the card's tag popover (language is shown as a code,
// and group/artist tags add little at a glance).
const HIDDEN_TAG_NAMESPACES = new Set(["language", "group", "artist"]);

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

/*
 * Mounted only while the tag popover is open. Keeping `useTagTranslation`
 * out of the card itself matters: every subscription made the whole grid
 * re-render the moment the translation database finished loading.
 */
function TagPopover({
  tags,
  position,
  onHoverChange,
}: {
  tags: string[];
  position: { left: number; bottom: number };
  onHoverChange: (hovering: boolean) => void;
}) {
  const { translateTag, ready } = useTagTranslation();

  return createPortal(
    <div
      className="fixed z-50 w-56 rounded-xl border border-kumo-hairline bg-kumo-elevated p-2 shadow-lg"
      style={{ left: position.left, bottom: position.bottom }}
      onMouseEnter={() => onHoverChange(true)}
      onMouseLeave={() => onHoverChange(false)}
    >
      <div className="flex flex-wrap gap-1">
        {tags.map((raw) => {
          const tag = parseTagString(raw);
          const label = ready
            ? translateTag(tag)
            : tag.namespace
              ? `${tag.namespace}:${tag.name}`
              : tag.name;
          return (
            <span
              key={raw}
              className="inline-block rounded-md bg-kumo-recessed px-1.5 py-0.5 text-[11px]"
            >
              {label}
            </span>
          );
        })}
      </div>
    </div>,
    document.body,
  );
}

export const GalleryCard = memo(function GalleryCard({
  gallery,
}: GalleryCardProps) {
  const [showTags, setShowTags] = useState(false);
  const [loadedImage, setLoadedImage] = useState<string | null>(null);
  const [popoverPos, setPopoverPos] = useState<{
    left: number;
    bottom: number;
  } | null>(null);
  const tagsRef = useRef<HTMLDivElement>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Tag parsing, language sniffing, image URL building and date formatting are
  // all derived once per gallery instead of on every render — with hundreds of
  // cards mounted, per-render work here shows up as scroll/animation frames.
  const derived = useMemo(() => {
    const tags = gallery.tags ?? [];
    const displayTags = tags.filter(
      (raw) => !HIDDEN_TAG_NAMESPACES.has(parseTagString(raw).namespace),
    );
    return {
      displayTags,
      hasTags: displayTags.length > 0,
      language: languageCodeFromTags(gallery.tags),
      hasPages: typeof gallery.pages === "number" && gallery.pages > 0,
      image: gallery.image ? thumbnailUrl(gallery.image) : "",
      posted: gallery.posted ? formatPosted(gallery.posted) : "",
      progress: gallery.reading
        ? gallery.reading.completed
          ? 100
          : Math.round(gallery.reading.progress * 100)
        : 0,
    };
  }, [gallery]);

  const { displayTags, hasTags, language, hasPages, image, posted, progress } =
    derived;
  const imageLoaded = image !== "" && loadedImage === image;
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

  // All dismiss handlers (outside click, scroll, resize) attach only while the
  // popover is open. A document listener per card meant N `contains()` checks
  // on every tap once several pages were scrolled in.
  useEffect(() => {
    if (!showTags) return;
    const handleClickOutside = (e: MouseEvent) => {
      if (tagsRef.current && !tagsRef.current.contains(e.target as Node)) {
        setShowTags(false);
      }
    };
    const close = () => setShowTags(false);
    const reposition = () => {
      const el = tagsRef.current;
      if (el) setPopoverPos(computePopoverPosition(el));
    };
    document.addEventListener("mousedown", handleClickOutside);
    window.addEventListener("scroll", close, { passive: true });
    window.addEventListener("resize", reposition, { passive: true });
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      window.removeEventListener("scroll", close);
      window.removeEventListener("resize", reposition);
    };
  }, [showTags]);

  return (
    <div className="gallery-card group relative">
      <div className="relative">
        <Link to={galleryHref} aria-label={gallery.title} className="block">
          <div className="relative aspect-[3/4] overflow-hidden rounded-xl bg-kumo-recessed ring-1 ring-kumo-hairline/70 transition-transform duration-200 group-hover:-translate-y-0.5 group-hover:shadow-lg">
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

            {/* progress bar: read portion solid, unread portion translucent */}
            {gallery.reading && (
              <div className="absolute inset-x-0 bottom-0 h-1 bg-[color-mix(in_oklab,var(--app-accent)_30%,transparent)]">
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
              className="flex size-7 items-center justify-center rounded-full bg-black/45 text-white ring-1 ring-white/25 transition-colors hover:bg-black/60"
              onClick={toggleTags}
              aria-label="查看标签"
              aria-expanded={showTags}
            >
              <TagIcon className="size-3.5" weight="fill" />
            </button>
          </div>
        )}

        {typeof gallery.rating === "number" && (
          <div className="pointer-events-none absolute right-0 top-0">
            <span className="inline-flex items-center gap-0.5 px-2 py-1 text-[11px] font-medium text-white [text-shadow:0_1px_3px_rgba(0,0,0,0.9)]">
              <Star
                className="size-3 text-amber-400 drop-shadow-[0_1px_2px_rgba(0,0,0,0.9)]"
                weight="fill"
              />
              {gallery.rating.toFixed(1)}
            </span>
          </div>
        )}

        {hasTags && showTags && popoverPos && (
          <TagPopover
            tags={displayTags}
            position={popoverPos}
            onHoverChange={(hovering) => {
              if (hovering) handleMouseEnter();
              else handleMouseLeave();
            }}
          />
        )}
      </div>

      <Link to={galleryHref} className="block">
        <div className="mt-2 space-y-1.5">
          <h3 className="line-clamp-2 min-h-10 text-sm font-medium leading-5">
            {gallery.title}
          </h3>

          <div className="flex items-center gap-2 text-xs text-kumo-subtle">
            {gallery.category && (
              <CategoryChip category={gallery.category} className="min-w-0 shrink" />
            )}
            {(language || hasPages) && (
              <span className="ml-auto flex shrink-0 items-center gap-1">
                {language && <span className="uppercase">{language}</span>}
                {hasPages && <span className="tnum">{gallery.pages}p</span>}
              </span>
            )}
          </div>

          {gallery.posted && (
            <p className="truncate text-right text-xs text-kumo-inactive">
              {posted}
            </p>
          )}
        </div>
      </Link>
    </div>
  );
});
