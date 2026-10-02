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
  galleryPageSlotsToManga,
  PageUrlStore,
  parsePageParam,
  slotPageSrcResolver,
} from "../lib/reader";
import {
  loadThumbnails,
  type LoadThumbnailsHandle,
  THUMBNAIL_CONCURRENCY,
} from "../lib/thumbnails";
import { ErrorState } from "../components/common/ErrorState";

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
  // Stable for the component's lifetime: page URLs stream in here while comimi
  // keeps a fixed set of page slots (see galleryPageSlotsToManga).
  const [pageUrlStore] = useState(() => new PageUrlStore());
  // How many pages the live stream has delivered so far; caps thumbnail
  // prefetch and is bumped without re-rendering the viewer.
  const receivedRef = useRef(0);
  const thumbnailsRef = useRef<LoadThumbnailsHandle | null>(null);
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
  // The gallery detail's page count is authoritative: it is fetched
  // independently of the streaming page list, so the reader's total never
  // changes as pages arrive.
  const total = gallery?.page_count || pagesQuery.data?.total || 0;
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

  // A new gallery resets the streaming store (and its pending resolvers). This
  // runs before the page-writing effect below so a fresh gallery always starts
  // empty.
  useEffect(() => {
    pageUrlStore.reset();
    receivedRef.current = 0;
    return () => pageUrlStore.reset();
  }, [pageUrlStore, id, token]);

  // Hand every newly streamed page URL to the store, then let the thumbnail
  // queue pick up any that just crossed the received-page limit.
  useEffect(() => {
    const list = pagesQuery.data?.pages ?? [];
    receivedRef.current = list.length;
    for (const page of list) pageUrlStore.set(page.index, page.page_url);
    thumbnailsRef.current?.refresh();
  }, [pageUrlStore, pagesQuery.data]);

  // Once the live list has settled short of the authoritative total, stop
  // waiting on the missing indexes so their slots show the error mascot.
  useEffect(() => {
    if (!pagesQuery.data || pagesQuery.isFetching || total <= 0) return;
    if (pagesQuery.data.pages.length < total) pageUrlStore.failAll();
  }, [pageUrlStore, pagesQuery.data, pagesQuery.isFetching, total]);

  const handlePageChange = useCallback(
    ({ pageIndex }: { pageIndex: number }) => {
      onPageChange(pageIndex);
    },
    [onPageChange],
  );

  // Fixed page slots from the authoritative total: comimi sees the full count
  // from the first render and never rebuilds its page list, so the total and
  // thumbnails stay put while the real page URLs stream into the store.
  const manga = useMemo(
    () =>
      gallery && total > 0
        ? galleryPageSlotsToManga(String(id), token ?? "", gallery.title, total)
        : null,
    [gallery, total, id, token],
  );

  // Stable resolver reads page URLs from the store as they arrive.
  const resolvePageSrc = useMemo(
    () => slotPageSrcResolver(pageUrlStore),
    [pageUrlStore],
  );

  const loading =
    galleryQuery.isLoading || pagesQuery.isLoading || progressQuery.isLoading;
  const error =
    galleryQuery.error ?? pagesQuery.error ?? progressQuery.error;

  // Route comimi's page-list / seek-preview thumbnails through a bounded
  // queue: they load as they appear, but never ahead of the streamed list.
  // Every URL is still handed to the queue lazily; only concurrency and the
  // received-page cap apply.
  useEffect(() => {
    if (!shellEl) return;
    const handle = loadThumbnails(shellEl, {
      concurrency: THUMBNAIL_CONCURRENCY,
      getLimit: () => receivedRef.current,
    });
    thumbnailsRef.current = handle;
    return () => {
      thumbnailsRef.current = null;
      handle();
    };
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

  if (!gallery || total === 0) {
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
          // Remount on gallery change so `initialPageIndex` applies to the new
          // gallery (comimi only reads it when the viewer is created).
          key={`${id}:${token ?? ""}`}
          ref={viewerRef}
          manga={manga!}
          initialPageIndex={initialPage}
          locale="zh-CN"
          storage={{ enabled: false }}
          settings={viewerSettings}
          resolvePageSrc={resolvePageSrc}
          onPageChange={handlePageChange}
          onLayoutChange={({ layoutMode: mode }) => setLayoutMode(mode)}
          className="h-full w-full"
        />
      </div>
    </div>
  );
}
