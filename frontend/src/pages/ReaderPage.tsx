import { useEffect, useMemo, useRef, useState } from "react";
import {
  useNavigate,
  useNavigationType,
  useParams,
  useSearchParams,
} from "react-router";
import { Button, Loader } from "@cloudflare/kumo";
import { ArrowLeft, Maximize, Minimize } from "lucide-react";
import {
  MangaViewer,
  type MangaViewerHandle,
  type ViewerSettings,
} from "@yui540/comimi-react";
import { useGallery, useGalleryPages, useReadingProgress } from "../hooks/useReaderData";
import { useReadingProgressSync } from "../hooks/useReadingProgressSync";
import { useTheme } from "../hooks/useTheme";
import { clampPageIndex, galleryPagesToManga } from "../lib/reader";
import { ErrorState } from "../components/common/ErrorState";

export function ReaderPage() {
  const { id: idParam, token } = useParams<{ id: string; token: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const navigationType = useNavigationType();
  const { resolvedMode } = useTheme();
  const viewerRef = useRef<MangaViewerHandle>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const id = Number(idParam);
  const restart = searchParams.get("restart") === "1";

  const isDark = resolvedMode === "dark";

  // 引用固定：comimi-react 按引用比较 settings，内联对象会导致每次翻页都全量重渲染
  const viewerSettings = useMemo<Partial<ViewerSettings>>(
    () => ({
      theme: isDark ? "dark" : "light",
      backgroundColor: isDark ? "black" : "white",
    }),
    [isDark],
  );

  // 真全屏由页面容器持有（顶栏保留在全屏内，按钮可退出）；
  // comimi 的 layoutMode（inline/wide/browserFullscreen）由库自理，两者互不干扰
  useEffect(() => {
    let isFs = false;
    const onChange = () => {
      isFs = document.fullscreenElement === containerRef.current;
      setIsFullscreen(isFs);
    };
    document.addEventListener("fullscreenchange", onChange);
    return () => {
      document.removeEventListener("fullscreenchange", onChange);
      if (isFs) void document.exitFullscreen();
    };
  }, []);

  const toggleFullscreen = () => {
    const el = containerRef.current;
    if (document.fullscreenElement === el) {
      void document.exitFullscreen();
      return;
    }
    if (!el) return;
    const viewer = viewerRef.current;
    // 宽屏布局在全屏里会留空隙，进入前静默归一为 inline；
    // 不 await 以保住用户手势的激活状态
    if (viewer && viewer.getState().layout.mode !== "inline") {
      void viewer.updateSettings({ layoutMode: "inline" });
    }
    // 老 Safari / iOS 等不支持元素全屏时降级为库内伪全屏，避免静默失败
    if (typeof el.requestFullscreen !== "function") {
      void viewer?.setLayoutMode("browserFullscreen");
      return;
    }
    el.requestFullscreen().catch(() => {
      void viewer?.setLayoutMode("browserFullscreen");
    });
  };

  const galleryQuery = useGallery(id, token ?? "");
  const pagesQuery = useGalleryPages(id, token ?? "");
  const progressQuery = useReadingProgress(id, token ?? "");

  const gallery = galleryQuery.data;
  const pages = pagesQuery.data?.pages;
  const total = pagesQuery.data?.total ?? 0;

  const initialPage = useMemo(() => {
    if (total <= 0) return 0;
    if (restart) return 0;
    return clampPageIndex(progressQuery.data?.current_page ?? 0, total);
  }, [total, restart, progressQuery.data?.current_page]);

  const { currentPage, onPageChange } = useReadingProgressSync(
    id,
    token ?? "",
    total,
    initialPage,
  );

  const manga = useMemo(
    () =>
      gallery && pages
        ? galleryPagesToManga(
            String(id),
            token ?? "",
            gallery.title,
            pages,
            gallery.thumbnail,
          )
        : null,
    [gallery, pages, id, token],
  );

  const loading =
    galleryQuery.isLoading || pagesQuery.isLoading || progressQuery.isLoading;
  const error =
    galleryQuery.error ?? pagesQuery.error ?? progressQuery.error;

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
        <Button variant="secondary" onClick={() => navigate(-1)}>
          <ArrowLeft className="mr-1 size-4" />
          返回
        </Button>
      </div>
    );
  }

  return (
    <div ref={containerRef} className="flex h-[100dvh] flex-col bg-kumo-base">
      {/* Top bar */}
      <div className="relative z-10 flex h-12 shrink-0 items-center gap-2 border-b border-kumo-hairline bg-kumo-elevated px-3">
        <Button
          variant="ghost"
          size="sm"
          onClick={() =>
            navigationType === "PUSH"
              ? navigate(-1)
              : navigate(`/gallery/${id}/${token}`)
          }
          aria-label="返回 Gallery"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <h1 className="min-w-0 flex-1 truncate text-sm font-medium">
          {gallery.title}
        </h1>
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
            <Minimize className="size-4" />
          ) : (
            <Maximize className="size-4" />
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
          onPageChange={({ pageIndex }) => onPageChange(pageIndex)}
          className="h-full w-full"
        />
      </div>
    </div>
  );
}
