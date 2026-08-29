import { useNavigate, useParams } from "react-router";
import { Badge, Button } from "@cloudflare/kumo";
import {
  ArrowLeft,
  Bookmark,
  BookmarkCheck,
  Book,
  Star,
  Loader2,
} from "lucide-react";
import {
  useBookshelfStatus,
  useBookshelfToggle,
  useGalleryDetail,
} from "../hooks/useGalleryDetail";
import { useReadingProgress } from "../hooks/useReaderData";
import { ErrorState } from "../components/common/ErrorState";
import { TagList } from "../components/tag";
import { thumbnailUrl } from "../lib/image";

function DetailSkeleton() {
  return (
    <div className="flex flex-col gap-6 md:flex-row md:gap-8">
      <div className="w-full shrink-0 md:w-64">
        <div className="aspect-[3/4] animate-pulse rounded-lg bg-kumo-recessed" />
      </div>
      <div className="flex-1 space-y-4">
        <div className="h-7 w-3/4 animate-pulse rounded bg-kumo-recessed" />
        <div className="h-5 w-1/2 animate-pulse rounded bg-kumo-recessed" />
        <div className="h-5 w-1/3 animate-pulse rounded bg-kumo-recessed" />
        <div className="h-10 w-40 animate-pulse rounded bg-kumo-recessed" />
      </div>
    </div>
  );
}

export function GalleryDetailPage() {
  const { id: idParam, token } = useParams<{ id: string; token: string }>();
  const navigate = useNavigate();
  const id = Number(idParam);

  const {
    data: gallery,
    isLoading,
    error,
    refetch,
  } = useGalleryDetail(id, token ?? "");
  const { data: shelfStatus } = useBookshelfStatus(id, token ?? "");
  const { add, remove } = useBookshelfToggle(id, token ?? "");
  const { data: progress } = useReadingProgress(id, token ?? "");

  const inShelf = shelfStatus?.in_bookshelf ?? false;
  const hasProgress = progress && (progress.current_page > 0 || progress.completed);

  if (isLoading) {
    return <DetailSkeleton />;
  }

  if (error || !gallery) {
    return (
      <ErrorState
        message={error?.message || "无法加载 Gallery"}
        onRetry={() => refetch()}
      />
    );
  }

  const togglePending = add.isPending || remove.isPending;

  const handleToggle = () => {
    if (inShelf) {
      remove.mutate();
    } else {
      add.mutate();
    }
  };

  return (
    <div className="space-y-6">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => navigate(-1)}
        className="w-fit"
      >
        <ArrowLeft className="mr-1 size-4" />
        返回
      </Button>

      <div className="flex flex-col gap-6 md:flex-row md:gap-8">
        {/* Cover */}
        <div className="w-full shrink-0 md:w-64">
          <div className="aspect-[3/4] overflow-hidden rounded-lg bg-kumo-recessed">
            <img
              src={thumbnailUrl(gallery.cover)}
              alt={gallery.title}
              className="h-full w-full object-cover"
              onError={(e) => {
                e.currentTarget.style.display = "none";
                e.currentTarget.nextElementSibling?.classList.remove("hidden");
              }}
            />
            <div className="hidden flex h-full items-center justify-center p-4 text-center text-xs text-kumo-subtle">
              Image unavailable
            </div>
          </div>
        </div>

        {/* Info */}
        <div className="min-w-0 flex-1 space-y-3">
          <h1 className="text-xl font-bold leading-snug">{gallery.title}</h1>
          {gallery.title_jpn && (
            <p className="text-sm text-kumo-subtle">{gallery.title_jpn}</p>
          )}

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
            <Badge variant="secondary">{gallery.category}</Badge>
            <span className="flex items-center gap-1">
              <Star className="size-4 fill-current text-kumo-warning" />
              {gallery.rating.toFixed(2)}
              {gallery.rating_count > 0 && (
                <span className="text-xs text-kumo-subtle">
                  ({gallery.rating_count})
                </span>
              )}
            </span>
            <span className="text-kumo-subtle">{gallery.page_count} 页</span>
          </div>

          <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-2">
            {gallery.uploader && (
              <div className="flex gap-2">
                <dt className="shrink-0 text-kumo-subtle">上传者</dt>
                <dd className="break-all">{gallery.uploader}</dd>
              </div>
            )}
            {gallery.posted && (
              <div className="flex gap-2">
                <dt className="shrink-0 text-kumo-subtle">上传时间</dt>
                <dd>{gallery.posted}</dd>
              </div>
            )}
            {gallery.language && (
              <div className="flex gap-2">
                <dt className="shrink-0 text-kumo-subtle">语言</dt>
                <dd>
                  {gallery.language}
                  {gallery.translated && (
                    <span className="ml-1 text-xs text-kumo-success">
                      (已翻译)
                    </span>
                  )}
                </dd>
              </div>
            )}
            {gallery.file_size && (
              <div className="flex gap-2">
                <dt className="shrink-0 text-kumo-subtle">文件大小</dt>
                <dd>{gallery.file_size}</dd>
              </div>
            )}
            {gallery.favorited > 0 && (
              <div className="flex gap-2">
                <dt className="shrink-0 text-kumo-subtle">收藏数</dt>
                <dd>{gallery.favorited}</dd>
              </div>
            )}
          </dl>

          <div className="flex flex-wrap items-center gap-3 pt-1">
            <Button
              variant="primary"
              onClick={() => navigate(`/reader/${id}/${token}`)}
            >
              <Book className="mr-1 size-4" />
              {progress?.completed
                ? "重新阅读"
                : hasProgress
                  ? `继续阅读 · 第 ${progress!.current_page + 1} 页`
                  : "开始阅读"}
            </Button>
            <Button
              variant={inShelf ? "secondary" : "outline"}
              onClick={handleToggle}
              disabled={togglePending}
              aria-label={inShelf ? "从书架移除" : "加入书架"}
            >
              {togglePending ? (
                <Loader2 className="mr-1 size-4 animate-spin" />
              ) : inShelf ? (
                <BookmarkCheck className="mr-1 size-4" />
              ) : (
                <Bookmark className="mr-1 size-4" />
              )}
              {inShelf ? "已收藏 · 点击移除" : "收藏到书架"}
            </Button>
          </div>
        </div>
      </div>

      {/* Tags */}
      {gallery.tags.length > 0 && (
        <section>
          <h2 className="mb-2 text-sm font-semibold text-kumo-subtle">标签</h2>
          <TagList tags={gallery.tags} />
        </section>
      )}
    </div>
  );
}
