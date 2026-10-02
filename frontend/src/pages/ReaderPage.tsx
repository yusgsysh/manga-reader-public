import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  useNavigate,
  useNavigationType,
  useParams,
  useSearchParams,
} from "react-router";
import { Button, Loader } from "@cloudflare/kumo";
import {
  ArrowLeft,
  CloudSlash,
  CornersOut,
  CornersIn,
} from "@phosphor-icons/react";
import {
  MangaViewer,
  type MangaViewerHandle,
  type ViewerSettings,
} from "@yui540/comimi-react";
import { useGallery, useGalleryPages, useReadingProgress } from "../hooks/useReaderData";
import { useReadingProgressSync } from "../hooks/useReadingProgressSync";
import { useTheme } from "../hooks/useTheme";
import {
  clampPageIndex,
  galleryPagesToManga,
  parsePageParam,
} from "../lib/reader";
import { loadThumbnails, THUMBNAIL_CONCURRENCY } from "../lib/thumbnails";
import { ErrorState } from "../components/common/ErrorState";
import type { GalleryPage } from "../types/reader";

type LayoutMode = ViewerSettings["layoutMode"];

export function ReaderPage() {
  const { id: idParam, token } = useParams<{ id: string; token: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const navigationType = useNavigationType();
  const { resolvedMode } = useTheme();
  const viewerRef = useRef<MangaViewerHandle>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  // The shell mounts only once the reader data is ready, which can happen
  // after `manga` is already set — keying the thumbnail watcher off the element
  // itself guarantees it starts with the shell instead of riding data changes
  // (and avoids restarting it every time `manga` changes identity).
  const [shellEl, setShellEl] = useState<HTMLDivElement | null>(null);
  const attachShell = useCallback((el: HTMLDivElement | null) => {
    containerRef.current = el;
    setShellEl(el);
  }, []);
  // The page the reader is actually on. The library resets to page 0 whenever
  // its `manga` prop changes (storage is disabled), so we keep the real index
  // here and restore it after the one-time completion update.
  const currentPageRef = useRef(0);
  const [viewerPages, setViewerPages] = useState<GalleryPage[] | null>(null);
  const [layoutMode, setLayoutMode] = useState<LayoutMode>("inline");
  const id = Number(idParam);
  const restart = searchParams.get("restart") === "1";
  // Capture a `?page=N` deep-link target once, so cleaning it from the URL
  // below cannot reset the reader back to the saved progress.
  const [pageOverride] = useState<number | null>(() =>
    parsePageParam(searchParams.get("page")),
  );

  const isDark = resolvedMode === "dark";
  const isFullscreen =
    layoutMode === "browserFullscreen" || layoutMode === "nativeFullscreen";

  // 引用固定：comimi-react 按引用比较 settings，内联对象会导致每次翻页都全量重渲染
  const viewerSettings = useMemo<Partial<ViewerSettings>>(
    () => ({
      theme: isDark ? "dark" : "light",
      backgroundColor: isDark ? "black" : "white",
      pageTurnMode: "single",
    }),
    [isDark],
  );

  // 全屏 = 库的 browserFullscreen 布局（跨平台一致、iOS 可用）叠加容器的
  // 原生全屏（可用时隐藏浏览器 UI）。退出全屏由库内 dock 的视图切换器负责。
  const toggleFullscreen = useCallback(() => {
    const viewer = viewerRef.current;
    if (!viewer) return;
    const mode = viewer.getState().layout.mode;
    const isFull =
      document.fullscreenElement !== null ||
      mode === "browserFullscreen" ||
      mode === "nativeFullscreen";
    if (isFull) {
      // setLayoutMode("inline") 会在必要时自行 exitFullscreen
      void viewer.setLayoutMode("inline");
      return;
    }
    void viewer.setLayoutMode("browserFullscreen");
    const el = containerRef.current;
    if (el && typeof el.requestFullscreen === "function") {
      el.requestFullscreen().catch(() => {});
    }
  }, []);

  // 用户通过浏览器/系统手势退出原生全屏时，库不会自行回到 inline，这里补齐。
  useEffect(() => {
    const handleFullscreenChange = () => {
      const viewer = viewerRef.current;
      if (!viewer || document.fullscreenElement) return;
      const mode = viewer.getState().layout.mode;
      if (mode === "nativeFullscreen" || mode === "browserFullscreen") {
        void viewer.setLayoutMode("inline");
      }
    };
    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () =>
      document.removeEventListener("fullscreenchange", handleFullscreenChange);
  }, []);

  // 离开路由时主动退出全屏，不依赖「节点移除浏览器自动退出」的时机
  useEffect(
    () => () => {
      if (document.fullscreenElement) {
        document.exitFullscreen().catch(() => {});
      }
    },
    [],
  );

  const galleryQuery = useGallery(id, token ?? "");
  const pagesQuery = useGalleryPages(id, token ?? "");
  const progressQuery = useReadingProgress(id, token ?? "");

  const gallery = galleryQuery.data;
  const pages = pagesQuery.data?.pages;
  const total = pagesQuery.data?.total ?? 0;
  // "离线数据" only when a cache value is actually being used as a fallback
  // (upstream failed or the browser is offline), not as a loading placeholder.
  const offline = galleryQuery.isFallback || pagesQuery.isFallback;

  const initialPage = useMemo(() => {
    if (total <= 0) return 0;
    if (pageOverride !== null) return clampPageIndex(pageOverride, total);
    if (restart) return 0;
    return clampPageIndex(progressQuery.data?.current_page ?? 0, total);
  }, [total, pageOverride, restart, progressQuery.data?.current_page]);

  // Drop the deep-link `page` param once captured so a refresh resumes from
  // the saved progress instead of jumping to the old target again.
  useEffect(() => {
    if (!searchParams.has("page")) return;
    const next = new URLSearchParams(searchParams);
    next.delete("page");
    setSearchParams(next, { replace: true });
  }, [searchParams, setSearchParams]);

  const { currentPage, onPageChange, flushProgress } = useReadingProgressSync(
    id,
    token ?? "",
    total,
    initialPage,
  );

  const pageCount = pages?.length ?? 0;
  // The reader mounts only once the batch containing the resume target has
  // arrived, so it can jump straight to the right page instead of clamping to
  // a partial list.
  const targetReady = !!pages && total > 0 && pageCount > initialPage;
  const listComplete = !!pages && total > 0 && pageCount >= total;

  useEffect(() => {
    currentPageRef.current = initialPage;
  }, [initialPage]);

  // Freeze the page list handed to the viewer: mount immediately on the first
  // snapshot that covers the target page so the first image shows without
  // waiting for the whole scrape, then refresh it exactly once when the full
  // list has arrived. Debouncing on every streamed batch would keep pushing
  // the snapshot back until the stream ended. Feeding every batch would make
  // comimi reset to page 0 on each update; this render-phase state adjustment
  // re-renders at most twice (first snapshot + completion).
  if (pages && targetReady) {
    if (viewerPages === null) {
      setViewerPages(pages);
    } else if (listComplete && viewerPages.length < pages.length) {
      setViewerPages(pages);
    }
  }

  const handlePageChange = useCallback(
    ({ pageIndex }: { pageIndex: number }) => {
      currentPageRef.current = pageIndex;
      onPageChange(pageIndex);
    },
    [onPageChange],
  );

  // comimi resets to page 0 on every setManga; after the single completion
  // update restore the page the reader was actually on.
  const handleMangaChange = useCallback(() => {
    requestAnimationFrame(() => {
      const viewer = viewerRef.current;
      const target = currentPageRef.current;
      if (viewer && viewer.getCurrentPageIndex() !== target) {
        viewer.goToPage(target);
      }
    });
  }, []);

  const manga = useMemo(
    () =>
      gallery && viewerPages
        ? galleryPagesToManga(String(id), token ?? "", gallery.title, viewerPages)
        : null,
    [gallery, viewerPages, id, token],
  );

  const loading =
    galleryQuery.isLoading || pagesQuery.isLoading || progressQuery.isLoading;
  const error =
    galleryQuery.error ?? pagesQuery.error ?? progressQuery.error;

  // Route comimi's page-list / seek-preview thumbnails through a bounded
  // queue: all of them load immediately, but concurrency stays capped.
  useEffect(() => {
    if (!shellEl) return;
    return loadThumbnails(shellEl, {
      concurrency: THUMBNAIL_CONCURRENCY,
    });
  }, [shellEl]);

  useEffect(() => {
    if (gallery?.title) {
      document.title = gallery.title;
    }
    return () => {
      document.title = "Manga Reader";
    };
  }, [gallery?.title]);

  if (loading) {
    return (
      <div className="flex h-[100dvh] flex-col items-center justify-center gap-4 bg-kumo-base">
        <Loader size={32} />
        <p className="text-sm text-kumo-subtle">正在加载漫画…</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-[100dvh] flex-col items-center justify-center gap-4 bg-kumo-base">
        <ErrorState
          message="无法加载阅读内容"
          onRetry={() => {
            galleryQuery.refetch();
            pagesQuery.refetch();
            progressQuery.refetch();
          }}
        />
      </div>
    );
  }

  if (!gallery || !pages || total === 0) {
    return (
      <div className="flex h-[100dvh] flex-col items-center justify-center gap-4 bg-kumo-base">
        <p className="text-sm text-kumo-subtle">没有可用的页面</p>
        <Button
          variant="secondary"
          onClick={() => {
            flushProgress();
            navigate(-1);
          }}
        >
          <ArrowLeft className="mr-1 size-4" weight="bold" />
          返回
        </Button>
      </div>
    );
  }

  // Keep the reader closed until the page list has streamed up to the resume
  // target, so mounting it cannot clamp to a not-yet-arrived page.
  if (!targetReady || !viewerPages) {
    return (
      <div className="flex h-[100dvh] flex-col items-center justify-center gap-4 bg-kumo-base">
        <Loader size={32} />
        <p className="text-sm text-kumo-subtle">
          正在加载到第 {initialPage + 1} 页…
        </p>
        <p className="tnum text-xs text-kumo-inactive">
          {pageCount} / {total} 页
        </p>
      </div>
    );
  }

  return (
    <div
      ref={attachShell}
      className="reader-shell flex h-[100dvh] flex-col bg-kumo-base"
    >
      {/* Top bar：全屏时由 comimi 接管，控件交给库内 dock */}
      <div className="reader-topbar relative z-10 flex min-h-12 shrink-0 items-center gap-2 border-b border-kumo-hairline bg-kumo-base/85 px-3 pt-[env(safe-area-inset-top)] backdrop-blur-md">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            flushProgress();
            if (navigationType === "PUSH") {
              navigate(-1);
            } else {
              navigate(`/gallery/${id}/${token}`);
            }
          }}
          aria-label="返回 Gallery"
        >
          <ArrowLeft className="size-4" weight="bold" />
        </Button>
        <div className="min-w-0 flex-1" />
        {offline && (
          <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-kumo-recessed px-2 py-0.5 text-[11px] text-kumo-subtle">
            <CloudSlash className="size-3" weight="bold" />
            离线数据
          </span>
        )}
        <span className="shrink-0 text-xs text-kumo-subtle">
          {currentPage + 1} / {total}
        </span>
        <Button
          variant="ghost"
          size="sm"
          onClick={toggleFullscreen}
          aria-label={isFullscreen ? "退出全屏" : "全屏"}
          title={isFullscreen ? "退出全屏" : "全屏"}
        >
          {isFullscreen ? (
            <CornersIn className="size-4" weight="bold" />
          ) : (
            <CornersOut className="size-4" weight="bold" />
          )}
        </Button>
      </div>

      {/* Reader */}
      <div className="min-h-0 flex-1">
        <MangaViewer
          ref={viewerRef}
          manga={manga!}
          initialPageIndex={initialPage}
          locale="zh-CN"
          storage={{ enabled: false }}
          settings={viewerSettings}
          onPageChange={handlePageChange}
          onMangaChange={handleMangaChange}
          onLayoutChange={({ layoutMode: mode }) => setLayoutMode(mode)}
          className="h-full w-full"
        />
      </div>
    </div>
  );
}
