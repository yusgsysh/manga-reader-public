import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
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
import {
  clampPageIndex,
  galleryPageSlotsToManga,
  PageUrlStore,
  parsePageParam,
  slotPageSrcResolver,
} from "../lib/reader";
import {
  DEFAULT_READER_SETTINGS,
  mergeReaderSettings,
  pickReaderSettings,
  sameReaderSettings,
  type ReaderSettings,
} from "../lib/readerSettings";
import { ErrorState } from "../components/common/ErrorState";
import { LoadingScreen } from "../components/common/LoadingScreen";
import { ReaderToolbar } from "../components/reader/ReaderToolbar";
import { useDocumentTitle } from "../hooks/useDocumentTitle";
import { getReaderPreferences } from "../lib/readerPrefs";
import {
  exitElementFullscreen,
  FULLSCREEN_CHANGE_EVENTS,
  getFullscreenElement,
  requestElementFullscreen,
} from "../lib/fullscreen";

export function ReaderPage() {
  const { id: idParam, token } = useParams<{ id: string; token: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const { resolvedMode } = useTheme();
  const viewerRef = useRef<MangaViewerHandle>(null);
  // The reader shell doubles as the native-fullscreen element: requesting it
  // (rather than `documentElement`) keeps the app top bar inside the fullscreen
  // subtree, so the exit button stays reachable on desktop.
  const shellRef = useRef<HTMLDivElement>(null);
  // Stable for the component's lifetime: page URLs stream in here while comimi
  // keeps a fixed set of page slots (see galleryPageSlotsToManga).
  const [pageUrlStore] = useState(() => new PageUrlStore());
  const id = Number(idParam);
  const goBack = useBackNavigation(`/gallery/${id}/${token}`);
  const restart = searchParams.get("restart") === "1";
  // Capture a `?page=N` deep-link target once, so cleaning it from the URL
  // below cannot reset the reader back to the saved progress.
  const [pageOverride] = useState<number | null>(() =>
    parsePageParam(searchParams.get("page")),
  );

  // Reader chrome (top bar) mirrors the active viewer state instead of owning
  // it. The viewer reports its page changes, and the chrome jumps through the
  // same handle.
  const [layoutMode, setLayoutMode] = useState<LayoutMode>("inline");
  const [nativeFullscreen, setNativeFullscreen] = useState(
    () => getFullscreenElement() !== null,
  );
  const [viewerReady, setViewerReady] = useState(false);
  const [readerSettings, setReaderSettings] = useState<ReaderSettings>(
    DEFAULT_READER_SETTINGS,
  );
  const [autoPageTurnEnabled, setAutoPageTurnEnabled] = useState(false);
  // Mirrors the active viewer's overlay state (the auto-hiding progress bar)
  // so the top bar can show and hide together with it.
  const [overlayVisible, setOverlayVisible] = useState(false);
  // Read once: changing the preference later must not yank the current reading
  // session into fullscreen, only the next one that is opened.
  const autoFullscreenOnOpenRef = useRef(
    getReaderPreferences().autoFullscreenOnStart,
  );

  const isDark = resolvedMode === "dark";

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

  // The comic page is the one reading position the page owns. The viewer
  // reports into it and is opened at it, so a remount never jumps.
  // `navigated` keeps the streamed progress from resetting a page the reader
  // has already moved to; the adjustment runs during render, before the
  // viewer reads the value.
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
    return () => pageUrlStore.reset();
  }, [pageUrlStore, id, token]);

  // Hand every newly streamed page URL to the store.
  useEffect(() => {
    const list = pagesQuery.data?.pages ?? [];
    for (const page of list) pageUrlStore.set(page.index, page.page_url);
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

  // 顶栏返回：先落盘进度，再按浏览器历史 / 深链回退规则返回。
  const handleBack = useCallback(() => {
    flushProgress();
    goBack();
  }, [flushProgress, goBack]);

  const handleGoToPage = useCallback(
    (pageIndex: number) => {
      const page = clampPageIndex(pageIndex, total);
      setNavigation({ page, navigated: true });
      viewerRef.current?.goToPage(page);
    },
    [total],
  );

  // The page list reads the live reading position instead of mirroring it into
  // state: the viewer keeps the source of truth, so the highlight can never lag
  // behind (e.g. after a viewer remount).
  const getCurrentPage = useCallback(() => readerPage, [readerPage]);

  const handleUpdateSettings = useCallback(
    (patch: Partial<ViewerSettings>) => {
      const next = mergeReaderSettings(readerSettings, {
        readingDirection: patch.readingDirection ?? readerSettings.readingDirection,
        pageTurnMode: patch.pageTurnMode ?? readerSettings.pageTurnMode,
        hasCover: patch.hasCover ?? readerSettings.hasCover,
        autoPageTurnIntervalMs:
          patch.autoPageTurnIntervalMs ?? readerSettings.autoPageTurnIntervalMs,
      });
      setReaderSettings(next);
      void viewerRef.current?.updateSettings(patch);
    },
    [readerSettings],
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
    viewerRef.current?.toggleAutoPageTurn();
  }, []);

  const handleSetOverlayVisible = useCallback((visible: boolean) => {
    viewerRef.current?.setOverlayVisible(visible);
  }, []);

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
    if (!viewer) return;
    if (!autoFullscreenOnOpenRef.current) return;
    if (viewer.getState().layout.mode !== "inline") return;
    if (getFullscreenElement()) {
      void viewer.updateSettings({ layoutMode: "browserFullscreen" });
    } else {
      void viewer.setLayoutMode("browserFullscreen");
    }
  }, [viewerReady]);

  const applyLayoutMode = useCallback((mode: LayoutMode) => {
    setLayoutMode(mode);
    void viewerRef.current?.setLayoutMode(mode);
  }, []);

  // 全屏 = 库的 browserFullscreen 布局（跨平台一致、iOS 可用）叠加容器的
  // 原生全屏（可用时隐藏浏览器 UI）。只设置布局模式在已经铺满视口的阅读器里
  // 看不出任何区别，所以进入时必须真的请求原生全屏；退出时由库的
  // setLayoutMode("inline") 负责 exitFullscreen。即使浏览器不支持原生全屏
  // （如 iOS），布局模式仍会切换，按钮和 F 键至少不会变成空操作。
  const toggleFullscreen = useCallback(() => {
    const isFull =
      getFullscreenElement() !== null ||
      layoutMode === "browserFullscreen" ||
      layoutMode === "nativeFullscreen";
    if (isFull) {
      applyLayoutMode("inline");
      void exitElementFullscreen();
      return;
    }
    applyLayoutMode("browserFullscreen");
    void requestElementFullscreen(shellRef.current);
  }, [applyLayoutMode, layoutMode]);

  // 用户通过浏览器/系统手势退出原生全屏时，库不会自行回到 inline，这里补齐。
  useEffect(() => {
    const handleFullscreenChange = () => {
      const active = getFullscreenElement() !== null;
      setNativeFullscreen(active);
      if (active) return;
      setLayoutMode((mode) =>
        mode === "nativeFullscreen" || mode === "browserFullscreen"
          ? "inline"
          : mode,
      );
    };
    for (const eventName of FULLSCREEN_CHANGE_EVENTS) {
      document.addEventListener(eventName, handleFullscreenChange);
    }
    return () => {
      for (const eventName of FULLSCREEN_CHANGE_EVENTS) {
        document.removeEventListener(eventName, handleFullscreenChange);
      }
    };
  }, []);

  // 离开路由时主动退出全屏，不依赖「节点移除浏览器自动退出」的时机
  useEffect(
    () => () => {
      void exitElementFullscreen();
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

  // comimi is seeded from the shared reader settings so the top bar can drive
  // direction, page mode and autoplay without a second settings store.
  const comimiSettings = useMemo<Partial<ViewerSettings>>(
    () => ({
      theme: isDark ? "dark" : "light",
      backgroundColor: isDark ? "black" : "white",
      layoutMode,
      ...readerSettings,
    }),
    [isDark, layoutMode, readerSettings],
  );

  const loading =
    galleryQuery.isLoading || pagesQuery.isLoading || progressQuery.isLoading;
  const error =
    galleryQuery.error ?? pagesQuery.error ?? progressQuery.error;

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
      ref={shellRef}
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
        onSetOverlayVisible={handleSetOverlayVisible}
        onGoToPage={handleGoToPage}
        onUpdateSettings={handleUpdateSettings}
      />

      {/* Reader：返回、全屏与阅读设置都在上方工具栏，comimi 不再渲染自己的菜单/设置面板 */}
      <div className="min-h-0 flex-1">
        <MangaViewer
          // Remount on gallery change so `initialPageIndex` applies to the new
          // gallery (comimi only reads it when the viewer is created).
          key={`${id}:${token ?? ""}`}
          ref={viewerRef}
          manga={manga!}
          initialPageIndex={readerPage}
          locale="zh-CN"
          storage={{ enabled: false }}
          settings={comimiSettings}
          resolvePageSrc={resolvePageSrc}
          onReady={handleReady}
          onPageChange={handlePageChange}
          onFullscreenRequest={toggleFullscreen}
          onLayoutChange={handleLayoutChange}
          onSettingsChange={handleSettingsChange}
          onAutoPageTurnChange={handleAutoPageTurnChange}
          onOverlayChange={handleOverlayChange}
          className="h-full w-full"
        />
      </div>
    </div>
  );
}
