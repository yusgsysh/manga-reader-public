import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { useParams, useSearchParams } from "react-router";
import { Button, Loader } from "@cloudflare/kumo";
import { ArrowLeft } from "@phosphor-icons/react";
import {
  MangaViewer,
  type LayoutMode,
  type MangaViewerHandle,
  type ViewerSettings,
} from "@yui540/comimi-react";
import { useGallery, useGalleryPages, useReadingProgress } from "../hooks/useReaderData";
import { useReadingProgressSync } from "../hooks/useReadingProgressSync";
import { useTheme } from "../hooks/useTheme";
import { useBackNavigation } from "../hooks/useBackNavigation";
import { useMediaQuery } from "../hooks/useMediaQuery";
import {
  clampPageIndex,
  galleryPageSlotsToManga,
  PageUrlStore,
  pageIndexSrcResolver,
  parsePageParam,
  slotPageSrcResolver,
} from "../lib/reader";
import { bookPageMap, positionForPage } from "../lib/bookPages";
import {
  DEFAULT_READER_SETTINGS,
  mergeReaderSettings,
  pickReaderSettings,
  sameReaderSettings,
  type ReaderSettings,
} from "../lib/readerSettings";
import {
  loadThumbnails,
  type LoadThumbnailsHandle,
  THUMBNAIL_CONCURRENCY,
} from "../lib/thumbnails";
import { ErrorState } from "../components/common/ErrorState";
import { LoadingScreen } from "../components/common/LoadingScreen";
import { ReaderToolbar } from "../components/reader/ReaderToolbar";
import { ReaderControlsBar } from "../components/reader/ReaderControlsBar";
import { ReaderSettingsPreference } from "../components/reader/ReaderSettingsPreference";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import { getReaderPreferences } from "../lib/readerPrefs";
import type { BookFlipViewerHandle } from "../components/reader/BookFlipViewer";

// The grabfold book view (and its WebGL renderer) is only downloaded when the
// reader is actually switched to "书本"; the paged and sliding modes never pay
// for it.
const LazyBookFlipViewer = lazy(() =>
  import("../components/reader/BookFlipViewer").then((module) => ({
    default: module.BookFlipViewer,
  })),
);

// 菜单底部返回入口的文案：补丁只引用 key，库内没有内置。
const READER_TRANSLATIONS = { "menu.backToGallery": "返回画廊" };

export function ReaderPage() {
  const { id: idParam, token } = useParams<{ id: string; token: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const { resolvedMode } = useTheme();
  const viewerRef = useRef<MangaViewerHandle>(null);
  const bookRef = useRef<BookFlipViewerHandle>(null);
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
  const id = Number(idParam);
  const goBack = useBackNavigation(`/gallery/${id}/${token}`);
  const restart = searchParams.get("restart") === "1";
  // Capture a `?page=N` deep-link target once, so cleaning it from the URL
  // below cannot reset the reader back to the saved progress.
  const [pageOverride] = useState<number | null>(() =>
    parsePageParam(searchParams.get("page")),
  );

  // Reader chrome (top bar) mirrors the active viewer state instead of owning
  // it. Both viewers are driven from the same comic page index.
  const [layoutMode, setLayoutMode] = useState<LayoutMode>("inline");
  const [nativeFullscreen, setNativeFullscreen] = useState(
    () => typeof document !== "undefined" && document.fullscreenElement !== null,
  );
  const [viewerReady, setViewerReady] = useState(false);
  const [readerSettings, setReaderSettings] = useState<ReaderSettings>(
    DEFAULT_READER_SETTINGS,
  );
  const [autoPageTurnEnabled, setAutoPageTurnEnabled] = useState(false);
  // Mirrors the active viewer's overlay state (the auto-hiding progress bar)
  // so the top bar can show and hide together with it.
  const [overlayVisible, setOverlayVisible] = useState(false);
  // The app-level preference is appended to comimi's own settings sheet.
  const [settingsHost, setSettingsHost] = useState<HTMLElement | null>(null);
  // Read once: changing the preference later must not yank the current book
  // into fullscreen, only the next one that is opened.
  const autoFullscreenOnOpenRef = useRef(
    getReaderPreferences().autoFullscreenOnStart,
  );

  const isDark = resolvedMode === "dark";
  const isBookMode = readerSettings.pageTurnAnimation === "book";

  const reducedMotion = useMediaQuery("(prefers-reduced-motion: reduce)");

  const galleryQuery = useGallery(id, token ?? "");
  const pagesQuery = useGalleryPages(id, token ?? "");
  const progressQuery = useReadingProgress(id, token ?? "");

  const gallery = galleryQuery.data;
  // The gallery detail's page count is authoritative: it is fetched
  // independently of the streaming page list, so the reader's total never
  // changes as pages arrive.
  const total = gallery?.page_count || pagesQuery.data?.total || 0;

  const initialPage = useMemo(() => {
    if (total <= 0) return 0;
    if (pageOverride !== null) return clampPageIndex(pageOverride, total);
    if (restart) return 0;
    return clampPageIndex(progressQuery.data?.current_page ?? 0, total);
  }, [total, pageOverride, restart, progressQuery.data?.current_page]);

  // The comic page is the one reading position the page owns. Both viewers
  // report into it, and either is opened at it, so a mode switch never jumps.
  // `navigated` keeps the streamed progress from resetting a page the reader
  // has already moved to; the adjustment runs during render, before the
  // viewers read the value.
  const [navigation, setNavigation] = useState({
    page: initialPage,
    navigated: false,
  });
  if (!navigation.navigated && navigation.page !== initialPage) {
    setNavigation({ page: initialPage, navigated: false });
  }
  const readerPage = navigation.page;

  // Drop the deep-link `page` param once captured so a refresh resumes from
  // the saved progress instead of jumping to the old target again.
  useEffect(() => {
    if (!searchParams.has("page")) return;
    const next = new URLSearchParams(searchParams);
    next.delete("page");
    setSearchParams(next, { replace: true });
  }, [searchParams, setSearchParams]);

  const { onPageChange, flushProgress } = useReadingProgressSync(
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

  const recordPage = useCallback((pageIndex: number) => {
    setNavigation({ page: pageIndex, navigated: true });
    onPageChange(pageIndex);
  }, [onPageChange]);

  const handlePageChange = useCallback(
    ({ pageIndex }: { pageIndex: number }) => {
      recordPage(pageIndex);
    },
    [recordPage],
  );

  // comimi 菜单里的返回入口：先落盘进度，再按浏览器历史 / 深链回退规则返回。
  const handleBack = useCallback(() => {
    flushProgress();
    goBack();
  }, [flushProgress, goBack]);

  // Close the library's own panel (reachable via its keyboard shortcuts) when
  // an app panel opens, so the two never overlap.
  const handleClosePanel = useCallback(() => {
    viewerRef.current?.setPanel("none");
  }, []);

  const handleGoToPage = useCallback(
    (pageIndex: number) => {
      const page = clampPageIndex(pageIndex, total);
      setNavigation({ page, navigated: true });
      if (isBookMode) bookRef.current?.goToPage(page);
      else viewerRef.current?.goToPage(page);
    },
    [isBookMode, total],
  );

  // The page list reads the live reading position instead of mirroring it into
  // state: the active viewer keeps the source of truth, so the highlight can
  // never lag behind (e.g. after a viewer remount on a mode change).
  const getCurrentPage = useCallback(() => readerPage, [readerPage]);

  // One step through the book (a spread in spread mode), never a page number
  // arithmetic that could land back inside the same spread.
  const handleTurnPage = useCallback(
    (direction: "previous" | "next") => {
      if (isBookMode) {
        if (direction === "next") bookRef.current?.next();
        else bookRef.current?.previous();
        return;
      }
      if (direction === "next") viewerRef.current?.nextPage();
      else viewerRef.current?.previousPage();
    },
    [isBookMode],
  );

  const handleUpdateSettings = useCallback(
    (patch: Partial<ViewerSettings>) => {
      const next = mergeReaderSettings(readerSettings, {
        readingDirection: patch.readingDirection ?? readerSettings.readingDirection,
        pageTurnMode: patch.pageTurnMode ?? readerSettings.pageTurnMode,
        pageTurnAnimation:
          patch.pageTurnAnimation ?? readerSettings.pageTurnAnimation,
        hasCover: patch.hasCover ?? readerSettings.hasCover,
        autoPageTurnIntervalMs:
          patch.autoPageTurnIntervalMs ?? readerSettings.autoPageTurnIntervalMs,
      });
      setReaderSettings(next);
      if (!isBookMode) void viewerRef.current?.updateSettings(patch);
    },
    [isBookMode, readerSettings],
  );

  const handleSettingsChange = useCallback(
    ({ settings }: { settings: ViewerSettings }) => {
      const next = pickReaderSettings(settings);
      setReaderSettings((current) =>
        sameReaderSettings(current, next) ? current : next,
      );
    },
    [],
  );

  const handleToggleAutoPageTurn = useCallback(() => {
    if (isBookMode) {
      setAutoPageTurnEnabled((enabled) => !enabled);
      return;
    }
    viewerRef.current?.toggleAutoPageTurn();
  }, [isBookMode]);

  const handleTogglePageTurnMode = useCallback(() => {
    handleUpdateSettings({
      pageTurnMode:
        readerSettings.pageTurnMode === "spread" ? "single" : "spread",
    });
  }, [handleUpdateSettings, readerSettings.pageTurnMode]);

  const handleToggleCover = useCallback(() => {
    handleUpdateSettings({ hasCover: !readerSettings.hasCover });
  }, [handleUpdateSettings, readerSettings.hasCover]);

  const handleReady = useCallback(() => {
    setViewerReady(true);
    if (autoPageTurnEnabled) viewerRef.current?.setAutoPageTurn(true);
  }, [autoPageTurnEnabled]);

  // Once comimi is ready, apply the "start in fullscreen" preference. The
  // starting click already asked for native fullscreen where possible; only
  // fall back to the library's layout when it did not take. This is an
  // opening-time decision, so it only runs on the first ready.
  const seededFullscreen = useRef(false);
  useEffect(() => {
    if (!viewerReady || seededFullscreen.current) return;
    seededFullscreen.current = true;
    const viewer = viewerRef.current;
    if (!viewer || isBookMode) return;
    if (!autoFullscreenOnOpenRef.current) return;
    if (viewer.getState().layout.mode !== "inline") return;
    if (document.fullscreenElement) {
      void viewer.updateSettings({ layoutMode: "browserFullscreen" });
    } else {
      void viewer.setLayoutMode("browserFullscreen");
    }
  }, [isBookMode, viewerReady]);

  const applyLayoutMode = useCallback((mode: LayoutMode) => {
    setLayoutMode(mode);
    if (!isBookMode) {
      void viewerRef.current?.setLayoutMode(mode);
      return;
    }
    // The book view has no comimi instance to lay out: fullscreen is just the
    // native API plus the shell's fullscreen styling.
    if (mode === "inline") {
      if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
      return;
    }
    const el = containerRef.current;
    if (el && typeof el.requestFullscreen === "function") {
      el.requestFullscreen().catch(() => {});
    }
  }, [isBookMode]);

  // 全屏 = 库的 browserFullscreen 布局（跨平台一致、iOS 可用）叠加容器的
  // 原生全屏（可用时隐藏浏览器 UI）。退出全屏由库内 dock 的视图切换器负责。
  const toggleFullscreen = useCallback(() => {
    const isFull =
      document.fullscreenElement !== null ||
      layoutMode === "browserFullscreen" ||
      layoutMode === "nativeFullscreen";
    applyLayoutMode(isFull ? "inline" : "browserFullscreen");
  }, [applyLayoutMode, layoutMode]);

  // 用户通过浏览器/系统手势退出原生全屏时，库不会自行回到 inline，这里补齐。
  useEffect(() => {
    const handleFullscreenChange = () => {
      const active = document.fullscreenElement !== null;
      setNativeFullscreen(active);
      if (active) return;
      setLayoutMode((mode) =>
        mode === "nativeFullscreen" || mode === "browserFullscreen"
          ? "inline"
          : mode,
      );
    };
    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () =>
      document.removeEventListener("fullscreenchange", handleFullscreenChange);
  }, []);

  // The app preference appends a row to comimi's settings sheet. The sheet is
  // created by the library after the viewer mounts, so wait for it instead of
  // assuming it exists. It is rebuilt whenever the viewer is remounted (for
  // example after leaving the book view), hence `isBookMode` in the deps.
  useEffect(() => {
    if (!shellEl || isBookMode) return;
    const findHost = () =>
      shellEl.querySelector<HTMLElement>(".comimi-settings-panel-inner");
    const attach = () => {
      const found = findHost();
      if (!found) return;
      observer.disconnect();
      setSettingsHost(found);
    };
    const observer = new MutationObserver(attach);
    observer.observe(shellEl, { childList: true, subtree: true });
    const frame = requestAnimationFrame(attach);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, [shellEl, isBookMode]);

  // 离开路由时主动退出全屏，不依赖「节点移除浏览器自动退出」的时机
  useEffect(
    () => () => {
      if (document.fullscreenElement) {
        document.exitFullscreen().catch(() => {});
      }
    },
    [],
  );

  const handleLayoutChange = useCallback(
    ({ layoutMode: nextMode }: { layoutMode: LayoutMode }) => {
      setLayoutMode(nextMode);
    },
    [],
  );

  const handleAutoPageTurnChange = useCallback(
    ({ enabled }: { enabled: boolean }) => {
      setAutoPageTurnEnabled(enabled);
    },
    [],
  );

  const handleOverlayChange = useCallback(
    ({ visible }: { visible: boolean }) => {
      setOverlayVisible(visible);
    },
    [],
  );

  const handleBookOverlayChange = useCallback((visible: boolean) => {
    setOverlayVisible(visible);
  }, []);

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
  const resolvePageIndexSrc = useMemo(
    () => pageIndexSrcResolver(pageUrlStore),
    [pageUrlStore],
  );

  // comimi is seeded from the shared reader settings, so handing the book view
  // over and back keeps direction, spread, cover and turn animation intact.
  const comimiSettings = useMemo<Partial<ViewerSettings>>(
    () => ({
      theme: isDark ? "dark" : "light",
      backgroundColor: isDark ? "black" : "white",
      layoutMode,
      ...readerSettings,
    }),
    [isDark, layoutMode, readerSettings],
  );

  // Book spreads share one position for two comic pages, so the control bar's
  // bounds come from the same map the book uses; slide mode is one page per
  // position.
  const turnBounds = useMemo(() => {
    if (!isBookMode) {
      return { canPrev: readerPage > 0, canNext: readerPage < total - 1 };
    }
    const map = bookPageMap(
      total,
      readerSettings.pageTurnMode,
      readerSettings.hasCover,
    );
    const position = positionForPage(map, readerPage);
    return { canPrev: position > map.first, canNext: position < map.last };
  }, [
    isBookMode,
    readerPage,
    total,
    readerSettings.pageTurnMode,
    readerSettings.hasCover,
  ]);

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

  useDocumentTitle(gallery?.title);

  if (loading) {
    return <LoadingScreen message="正在加载漫画…" />;
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
            goBack();
          }}
        >
          <ArrowLeft className="mr-1 size-4" weight="bold" />
          返回
        </Button>
      </div>
    );
  }

  const isFullscreen = layoutMode !== "inline" || nativeFullscreen;

  return (
    <div
      ref={attachShell}
      data-fullscreen={isFullscreen}
      className="reader-shell relative flex h-[100dvh] flex-col bg-kumo-base"
    >
      <ReaderToolbar
        title={gallery.title}
        fullscreen={isFullscreen}
        overlayVisible={overlayVisible}
        viewerSettings={readerSettings}
        id={id}
        token={token ?? ""}
        total={total}
        getCurrentPage={getCurrentPage}
        autoPageTurnEnabled={autoPageTurnEnabled}
        onBack={handleBack}
        onToggleFullscreen={toggleFullscreen}
        onToggleAutoPageTurn={handleToggleAutoPageTurn}
        onGoToPage={handleGoToPage}
        onClosePanel={handleClosePanel}
        onUpdateSettings={handleUpdateSettings}
      />

      {/* Reader：返回与全屏入口在上方工具栏，comimi 自己的浮动菜单头行已隐藏 */}
      <div className="min-h-0 flex-1">
        {isBookMode ? (
          <Suspense
            fallback={
              <div className="relative h-full w-full">
                <Loader
                  size={32}
                  aria-label="正在加载书本"
                  className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2"
                />
              </div>
            }
          >
            <LazyBookFlipViewer
              // The book is opening at the page the reader was already on;
              // remount only when the gallery changes.
              key={`${id}:${token ?? ""}`}
              ref={bookRef}
              total={total}
              initialPage={readerPage}
              readingDirection={readerSettings.readingDirection}
              pageTurnMode={readerSettings.pageTurnMode}
              hasCover={readerSettings.hasCover}
              autoPageTurn={autoPageTurnEnabled}
              autoPageTurnIntervalMs={readerSettings.autoPageTurnIntervalMs}
              reducedMotion={reducedMotion}
              resolvePageSrc={resolvePageIndexSrc}
              onPageChange={recordPage}
              onOverlayChange={handleBookOverlayChange}
              onToggleFullscreen={toggleFullscreen}
              onToggleAutoPageTurn={handleToggleAutoPageTurn}
              onTogglePageTurnMode={handleTogglePageTurnMode}
              onToggleCover={handleToggleCover}
            />
          </Suspense>
        ) : (
          <MangaViewer
            // Remount on gallery change so `initialPageIndex` applies to the new
            // gallery (comimi only reads it when the viewer is created).
            key={`${id}:${token ?? ""}`}
            ref={viewerRef}
            manga={manga!}
            initialPageIndex={readerPage}
            locale="zh-CN"
            translations={READER_TRANSLATIONS}
            storage={{ enabled: false }}
            settings={comimiSettings}
            resolvePageSrc={resolvePageSrc}
            onReady={handleReady}
            onPageChange={handlePageChange}
            onBack={handleBack}
            onFullscreenRequest={toggleFullscreen}
            onLayoutChange={handleLayoutChange}
            onSettingsChange={handleSettingsChange}
            onAutoPageTurnChange={handleAutoPageTurnChange}
            onOverlayChange={handleOverlayChange}
            className="h-full w-full"
          />
        )}
      </div>

      {/* One control bar for both viewers: slide and book modes never swap
          control sets, and comimi's own dock/arrows are hidden in the shell. */}
      <ReaderControlsBar
        page={readerPage}
        total={total}
        visible={overlayVisible}
        readingDirection={readerSettings.readingDirection}
        canPrev={turnBounds.canPrev}
        canNext={turnBounds.canNext}
        onTurnPage={handleTurnPage}
        onGoToPage={handleGoToPage}
      />

      {/* App-level reader preference, appended to comimi's settings sheet. */}
      {settingsHost &&
        createPortal(<ReaderSettingsPreference />, settingsHost)}
    </div>
  );
}
