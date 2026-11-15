import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useParams, useSearchParams } from "react-router";
import { Button } from "@cloudflare/kumo";
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
  parsePageParam,
  slotPageSrcResolver,
} from "../lib/reader";
import {
  loadThumbnails,
  type LoadThumbnailsHandle,
  THUMBNAIL_CONCURRENCY,
} from "../lib/thumbnails";
import { ErrorState } from "../components/common/ErrorState";
import { LoadingScreen } from "../components/common/LoadingScreen";
import { ReaderToolbar } from "../components/reader/ReaderToolbar";
import { ReaderSettingsPreference } from "../components/reader/ReaderSettingsPreference";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import { useBookFold } from "../hooks/useBookFold";
import { getReaderPreferences } from "../lib/readerPrefs";

// 菜单底部返回入口的文案：补丁只引用 key，库内没有内置。
const READER_TRANSLATIONS = { "menu.backToGallery": "返回画廊" };

export function ReaderPage() {
  const { id: idParam, token } = useParams<{ id: string; token: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
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
  const id = Number(idParam);
  const goBack = useBackNavigation(`/gallery/${id}/${token}`);
  const restart = searchParams.get("restart") === "1";
  // Capture a `?page=N` deep-link target once, so cleaning it from the URL
  // below cannot reset the reader back to the saved progress.
  const [pageOverride] = useState<number | null>(() =>
    parsePageParam(searchParams.get("page")),
  );

  // Reader chrome (top bar) mirrors the viewer state instead of owning it.
  const [layoutMode, setLayoutMode] = useState<LayoutMode>("inline");
  const [nativeFullscreen, setNativeFullscreen] = useState(
    () => typeof document !== "undefined" && document.fullscreenElement !== null,
  );
  const [viewerReady, setViewerReady] = useState(false);
  const [viewerSettingsState, setViewerSettingsState] =
    useState<ViewerSettings | null>(null);
  const [autoPageTurnEnabled, setAutoPageTurnEnabled] = useState(false);
  // Mirrors comimi's overlay state (the auto-hiding progress bar + arrows) so
  // the top bar can show and hide together with it.
  const [overlayVisible, setOverlayVisible] = useState(false);
  // The app-level preference is appended to comimi's own settings sheet.
  const [settingsHost, setSettingsHost] = useState<HTMLElement | null>(null);
  // Read once: changing the preference later must not yank the current book
  // into fullscreen, only the next one that is opened.
  const autoFullscreenOnOpenRef = useRef(
    getReaderPreferences().autoFullscreenOnStart,
  );

  const isDark = resolvedMode === "dark";

  // 引用固定：comimi-react 按引用比较 settings，内联对象会导致每次翻页都全量重渲染
  const viewerSettings = useMemo<Partial<ViewerSettings>>(
    () => ({
      theme: isDark ? "dark" : "light",
      backgroundColor: isDark ? "black" : "white",
      pageTurnMode: "single",
      // 翻页动画：默认「平移」，可在「阅读设置」切换为「书本翻页」。
      pageTurnAnimation: "slide",
      // 打开时默认「标准」，可点 dock 的全屏按钮进入全屏。
      layoutMode: "inline",
    }),
    [isDark],
  );

  const reducedMotion = useMediaQuery("(prefers-reduced-motion: reduce)");

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
      const active = document.fullscreenElement !== null;
      setNativeFullscreen(active);
      const viewer = viewerRef.current;
      if (!viewer || active) return;
      const mode = viewer.getState().layout.mode;
      if (mode === "nativeFullscreen" || mode === "browserFullscreen") {
        void viewer.setLayoutMode("inline");
      }
    };
    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () =>
      document.removeEventListener("fullscreenchange", handleFullscreenChange);
  }, []);

  // The app preference appends a row to comimi's settings sheet. The sheet is
  // created by the library after the viewer mounts, so wait for it instead of
  // assuming it exists.
  useEffect(() => {
    if (!shellEl) return;
    const findHost = () =>
      shellEl.querySelector<HTMLElement>(".comimi-settings-panel-inner");
    const attach = () => {
      const found = findHost();
      if (!found) return;
      observer.disconnect();
      setSettingsHost(found);
    };
    // The sheet is built after the viewer's first update, and on a cached
    // mount it may already be there before this effect runs. Check on the next
    // frame and listen for the rest instead of setting state synchronously.
    const observer = new MutationObserver(attach);
    observer.observe(shellEl, { childList: true, subtree: true });
    const frame = requestAnimationFrame(attach);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, [shellEl]);

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

  const handlePageChange = useCallback(
    ({ pageIndex }: { pageIndex: number }) => {
      onPageChange(pageIndex);
    },
    [onPageChange],
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

  const handleGoToPage = useCallback((pageIndex: number) => {
    viewerRef.current?.goToPage(pageIndex);
  }, []);

  // The page list reads the live viewer position instead of mirroring it into
  // state: comimi keeps the source of truth, so the highlight can never lag
  // behind (e.g. after the viewer remounts on a gallery change).
  const getCurrentPage = useCallback(
    () => viewerRef.current?.getState().currentPageIndex ?? 0,
    [],
  );

  const handleUpdateSettings = useCallback(
    (patch: Partial<ViewerSettings>) => {
      void viewerRef.current?.updateSettings(patch);
    },
    [],
  );

  const handleToggleAutoPageTurn = useCallback(() => {
    viewerRef.current?.toggleAutoPageTurn();
  }, []);

  const handleReady = useCallback(() => {
    setViewerReady(true);
  }, []);

  // Once the viewer is ready, seed the chrome from its state and apply the
  // "start in fullscreen" preference. The comimi-react instance is only
  // available after its mount effect, so this cannot run on the first commit.
  useEffect(() => {
    if (!viewerReady) return;
    const viewer = viewerRef.current;
    if (!viewer) return;
    const state = viewer.getState();
    setViewerSettingsState(state.settings);
    setLayoutMode(state.layout.mode);
    setAutoPageTurnEnabled(state.autoPageTurnEnabled);
    if (autoFullscreenOnOpenRef.current && state.layout.mode === "inline") {
      // The gallery's start-reading click already asked for native fullscreen
      // (`requestReaderFullscreenIfEnabled`). comimi's
      // `setLayoutMode("browserFullscreen")` exits any active native fullscreen,
      // which would undo that request — the "enter then immediately exit"
      // flicker. When native fullscreen is already on, apply the fullscreen
      // layout via `updateSettings` (it only changes `layout.mode`, with no
      // fullscreen side effects). Otherwise fall back to the library layout so
      // the reader still fills the window when the browser refused.
      if (document.fullscreenElement) {
        void viewer.updateSettings({ layoutMode: "browserFullscreen" });
      } else {
        void viewer.setLayoutMode("browserFullscreen");
      }
    }
  }, [viewerReady]);

  const handleLayoutChange = useCallback(
    ({ layoutMode: nextMode }: { layoutMode: LayoutMode }) => {
      setLayoutMode(nextMode);
    },
    [],
  );

  const handleSettingsChange = useCallback(
    ({ settings }: { settings: ViewerSettings }) => {
      setViewerSettingsState(settings);
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

  // Enhance the "书本翻页" animation with grabfold-style folding.
  useBookFold(shellEl, { reducedMotion });

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
        viewerSettings={viewerSettingsState}
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
        <MangaViewer
          // Remount on gallery change so `initialPageIndex` applies to the new
          // gallery (comimi only reads it when the viewer is created).
          key={`${id}:${token ?? ""}`}
          ref={viewerRef}
          manga={manga!}
          initialPageIndex={initialPage}
          locale="zh-CN"
          translations={READER_TRANSLATIONS}
          storage={{ enabled: false }}
          settings={viewerSettings}
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
      </div>

      {/* App-level reader preference, appended to comimi's settings sheet. */}
      {settingsHost &&
        createPortal(<ReaderSettingsPreference />, settingsHost)}
    </div>
  );
}
