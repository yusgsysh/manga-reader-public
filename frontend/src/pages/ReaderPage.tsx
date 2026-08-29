import { useEffect, useMemo } from "react";
import {
  useNavigate,
  useNavigationType,
  useParams,
  useSearchParams,
} from "react-router";
import { Button, Loader } from "@cloudflare/kumo";
import { ArrowLeft } from "lucide-react";
import { MangaViewer } from "@yui540/comimi-react";
import { useGallery, useGalleryPages, useReadingProgress } from "../hooks/useReaderData";
import { useReadingProgressSync } from "../hooks/useReadingProgressSync";
import { clampPageIndex, galleryPagesToManga } from "../lib/reader";
import { ErrorState } from "../components/common/ErrorState";

export function ReaderPage() {
  const { id: idParam, token } = useParams<{ id: string; token: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const navigationType = useNavigationType();
  const id = Number(idParam);
  const restart = searchParams.get("restart") === "1";

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
    <div className="flex h-[100dvh] flex-col bg-kumo-base">
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
      </div>

      {/* Reader */}
      <div className="min-h-0 flex-1">
        <MangaViewer
          manga={manga!}
          initialPageIndex={initialPage}
          locale="ja"
          storage={{ enabled: false }}
          onPageChange={({ pageIndex }) => onPageChange(pageIndex)}
          className="h-full w-full"
        />
      </div>
    </div>
  );
}
