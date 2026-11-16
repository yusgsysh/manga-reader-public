import { useEffect, useRef, useState } from "react";
import { pageThumbnailUrl } from "../../lib/image";
import { loadThumbnails, THUMBNAIL_CONCURRENCY } from "../../lib/thumbnails";

/*
 * App-owned reader panels, rendered from the reader top bar.
 *
 * comimi no longer renders a menu or settings panel here: its hidden native
 * chrome was removed in the patch, so these app-owned panels are the only ones.
 * The top bar (ReaderToolbar) anchors them to the top-right buttons.
 */

/* ------------------------------------------------------------------ */
/* Keyboard shortcuts                                                  */
/* ------------------------------------------------------------------ */

interface Shortcut {
  keys: string[];
  label: string;
}

interface ShortcutSection {
  title: string;
  /** Wrap items into a flowing multi-column list instead of one per row. */
  wrap?: boolean;
  items: Shortcut[];
}

// Mirrors comimi's own shortcut sheet (`buildShortcutView`), with the labels
// the app already shows elsewhere. "显示模式" is shown because the app does
// not lock the layout mode.
const SHORTCUT_SECTIONS: ShortcutSection[] = [
  {
    title: "翻页",
    items: [
      { keys: ["←", "Space"], label: "向左移动" },
      { keys: ["→", "Space + Shift"], label: "向右移动" },
      { keys: ["A"], label: "自动播放" },
    ],
  },
  {
    title: "显示模式",
    wrap: true,
    items: [
      { keys: ["N"], label: "标准" },
      { keys: ["F"], label: "全屏" },
      { keys: ["Esc"], label: "关闭面板 / 退出全屏" },
    ],
  },
  {
    title: "通用",
    wrap: true,
    items: [
      { keys: ["O"], label: "切换控制层" },
      { keys: ["P"], label: "切换跨页" },
      { keys: ["C"], label: "切换封面" },
    ],
  },
];

function KeyBadge({ keys }: { keys: string[] }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {keys.map((key, index) => (
        <span key={key} className="inline-flex items-center gap-1.5">
          {index > 0 && (
            <span className="text-[11px] text-kumo-subtle" aria-hidden>
              /
            </span>
          )}
          <kbd className="min-w-5 rounded-md bg-kumo-recessed px-1.5 py-0.5 text-center text-xs font-semibold text-kumo-default">
            {key}
          </kbd>
        </span>
      ))}
    </span>
  );
}

export function ReaderShortcuts() {
  return (
    <div className="space-y-5">
      {SHORTCUT_SECTIONS.map((section) => (
        <div key={section.title}>
          <div className="mb-2 text-xs font-semibold text-kumo-subtle">
            {section.title}
          </div>
          <div
            className={
              section.wrap
                ? "flex flex-wrap gap-x-5 gap-y-2.5"
                : "space-y-2.5"
            }
          >
            {section.items.map((item) => (
              <div key={item.label} className="flex items-center gap-2.5">
                <KeyBadge keys={item.keys} />
                <span className="text-sm text-kumo-default">{item.label}</span>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

/* ------------------------------------------------------------------ */
/* Page list                                                           */
/* ------------------------------------------------------------------ */

interface ReaderPageListProps {
  id: number;
  token: string;
  total: number;
  currentPage: number;
  onGoToPage: (pageIndex: number) => void;
}

function PageThumb({
  id,
  token,
  index,
  active,
  onSelect,
}: {
  id: number;
  token: string;
  index: number;
  active: boolean;
  onSelect: (pageIndex: number) => void;
}) {
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  // Index-addressed thumbnail; the backend resolves the sprite itself.
  const src = token ? pageThumbnailUrl(id, token, index) : "";
  const showShimmer = src !== "" && !loaded && !failed;

  return (
    <button
      type="button"
      aria-label={`第 ${index + 1} 页`}
      aria-current={active ? "page" : undefined}
      onClick={() => onSelect(index)}
      className="reader-page-list-item group relative block aspect-[100/141] overflow-hidden rounded-md border border-kumo-hairline bg-kumo-recessed transition hover:-translate-y-0.5 hover:shadow-md focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--app-accent)]"
    >
      {src && !failed ? (
        <img
          src={src}
          alt={`第 ${index + 1} 页`}
          decoding="async"
          className="app-image block h-full w-full object-cover"
          onLoad={() => setLoaded(true)}
          onError={() => {
            setFailed(true);
            setLoaded(true);
          }}
        />
      ) : (
        <span
          className="block h-full w-full bg-gradient-to-br from-kumo-recessed to-kumo-tint"
          aria-hidden
        />
      )}
      {showShimmer && (
        // The wrapper is absolute while .skeleton-shimmer keeps
        // `position: relative`, so the shimmer fills the tile. Use block-level
        // <div>s (like the gallery grid): `position: relative` does not
        // blockify an inline <span>, so `h-full w-full` would be ignored and
        // the placeholder would collapse to nothing.
        <div className="absolute inset-0 overflow-hidden" aria-hidden>
          <div className="skeleton-shimmer h-full w-full bg-kumo-recessed" />
        </div>
      )}
      <span className="absolute right-1 bottom-1 rounded-full bg-black/65 px-1.5 py-0.5 text-[11px] font-semibold leading-relaxed text-white tabular-nums">
        {index + 1}
      </span>
      {active && (
        <span
          className="absolute inset-0 rounded-md ring-2 ring-[var(--app-accent)]"
          aria-hidden
        />
      )}
    </button>
  );
}

export function ReaderPageList({
  id,
  token,
  total,
  currentPage,
  onGoToPage,
}: ReaderPageListProps) {
  const listRef = useRef<HTMLDivElement>(null);

  // The top-bar page list is a user-opened view of every page, so its
  // index-addressed thumbnails load through their own concurrency-limited queue.
  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    return loadThumbnails(el, { concurrency: THUMBNAIL_CONCURRENCY });
  }, []);

  // Centre the current page when the list opens so deep progress is visible.
  useEffect(() => {
    const container = listRef.current;
    const active =
      container?.querySelector<HTMLElement>('[aria-current="page"]');
    if (!container || !active) return;
    const target =
      active.offsetTop - container.clientHeight / 2 + active.offsetHeight / 2;
    container.scrollTop = Math.max(0, target);
  }, []);

  return (
    <div
      ref={listRef}
      className="reader-page-list relative grid max-h-[60vh] grid-cols-4 gap-2.5 overflow-y-auto overscroll-contain pr-1 sm:grid-cols-5"
    >
      {Array.from({ length: total }, (_, index) => (
        <PageThumb
          key={index}
          id={id}
          token={token}
          index={index}
          active={index === currentPage}
          onSelect={onGoToPage}
        />
      ))}
    </div>
  );
}
