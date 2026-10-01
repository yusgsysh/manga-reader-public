import { useState } from "react";
import { useNavigate, useParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Button, useKumoToastManager } from "@cloudflare/kumo";
import {
  ArrowLeft,
  BookmarkSimple,
  BookOpen,
  CloudSlash,
  DownloadSimple,
  Star,
  CircleNotch,
} from "@phosphor-icons/react";

import {
  useBookshelfStatus,
  useBookshelfToggle,
  useGalleryDetail,
} from "../hooks/useGalleryDetail";
import { fetchGalleryPagesWithFallback } from "../api/gallery";
import { useGalleryPages, useReadingProgress } from "../hooks/useReaderData";
import { useStartPrefillJob } from "../hooks/usePrefillJobs";
import { ErrorState } from "../components/common/ErrorState";
import { PageThumbnailGrid } from "../components/gallery/PageThumbnailGrid";
import { TagList } from "../components/tag";
import { thumbnailUrl } from "../lib/image";
import { formatPosted } from "../lib/time";
import { Chip, Section } from "../components/ui";

function PageGridSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 2xl:grid-cols-6">
      {Array.from({ length: 12 }).map((_, index) => (
        <div
          key={index}
          className="aspect-[2/3] animate-pulse rounded-xl bg-kumo-recessed"
        />
      ))}
    </div>
  );
}

function DetailSkeleton() {
  return (
    <div className="space-y-8">
      <div className="h-8 w-20 animate-pulse rounded bg-kumo-recessed" />

      <div className="space-y-6">
        <div className="flex flex-col gap-6 sm:flex-row">
          <div className="mx-auto w-full max-w-[220px] shrink-0 sm:mx-0 sm:w-[220px]">
            <div className="aspect-[3/4] animate-pulse rounded-2xl bg-kumo-recessed" />
          </div>

          <div className="min-w-0 flex-1 space-y-5">
            <div className="space-y-2">
              <div className="h-8 w-3/4 animate-pulse rounded bg-kumo-recessed" />
              <div className="h-5 w-1/2 animate-pulse rounded bg-kumo-recessed" />
            </div>

            <div className="h-6 w-56 animate-pulse rounded bg-kumo-recessed" />

            <div className="flex gap-2">
              <div className="h-9 w-28 animate-pulse rounded bg-kumo-recessed" />
              <div className="h-9 w-20 animate-pulse rounded bg-kumo-recessed" />
              <div className="h-9 w-20 animate-pulse rounded bg-kumo-recessed" />
            </div>

            <div className="grid grid-cols-1 gap-3 border-t border-kumo-hairline pt-4 sm:grid-cols-2">
              {Array.from({ length: 6 }).map((_, index) => (
                <div
                  key={index}
                  className="h-5 animate-pulse rounded bg-kumo-recessed"
                />
              ))}
            </div>
          </div>
        </div>

        <div className="space-y-3">
          <div className="h-5 w-12 animate-pulse rounded bg-kumo-recessed" />
          <div className="flex flex-wrap gap-2">
            {Array.from({ length: 8 }).map((_, index) => (
              <div
                key={index}
                className="h-7 w-20 animate-pulse rounded-full bg-kumo-recessed"
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

export function GalleryDetailPage() {
  const { id: idParam, token } = useParams<{
    id: string;
    token: string;
  }>();

  const navigate = useNavigate();
  const id = Number(idParam);

  const {
    data: gallery,
    isFallback,
    isLoading,
    error,
    refetch,
  } = useGalleryDetail(id, token ?? "");

  const { data: shelfStatus } = useBookshelfStatus(id, token ?? "");
  const { add, remove } = useBookshelfToggle(id, token ?? "");
  const { data: progress } = useReadingProgress(id, token ?? "");
  const pagesQuery = useGalleryPages(id, token ?? "");
  const startPrefill = useStartPrefillJob();
  const queryClient = useQueryClient();
  const toast = useKumoToastManager();

  const [preparingDownload, setPreparingDownload] = useState(false);

  const inShelf = shelfStatus?.in_bookshelf ?? false;
  const hasProgress =
    progress && (progress.current_page > 0 || progress.completed);

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
      return;
    }

    add.mutate(undefined, {
      onSuccess: (result) => {
        if (result.offline) {
          toast.add({
            title: "已离线加入书架",
            description: "书籍信息来自本地缓存",
            variant: "success",
          });
        }
      },
      onError: (error) => {
        const message =
          error instanceof Error ? error.message : String(error);

        toast.add({
          title: "加入书架失败",
          description: message,
          variant: "error",
        });
      },
    });
  };

  const downloadPending = preparingDownload || startPrefill.isPending;

  const handleDownload = async () => {
    if (downloadPending) return;

    setPreparingDownload(true);

    try {
      const pagesData = await queryClient.fetchQuery({
        queryKey: ["download-pages", id, token ?? ""],
        queryFn: () => fetchGalleryPagesWithFallback(id, token ?? ""),
        staleTime: 5 * 60_000,
      });

      startPrefill.mutate(
        {
          gallery_id: id,
          gallery_token: token ?? "",
          title: gallery.title,
          urls: pagesData.pages.map((p) => p.page_url),
        },
        {
          onSuccess: (job) => {
            toast.add({
              title: "已添加下载任务",
              description: `共 ${job.total} 页 · 在「下载管理」查看进度`,
              variant: "success",
            });
          },
          onError: (error) => {
            const message =
              error instanceof Error ? error.message : String(error);

            toast.add({
              title: "创建下载任务失败",
              description: message,
              variant: "error",
            });
          },
        },
      );
    } catch (error) {
      const message =
        error instanceof Error ? error.message : String(error);

      toast.add({
        title: "获取页面列表失败",
        description: message,
        variant: "error",
      });
    } finally {
      setPreparingDownload(false);
    }
  };

  const meta: { label: string; value: React.ReactNode }[] = [];

  if (gallery.uploader) {
    meta.push({
      label: "上传者",
      value: gallery.uploader,
    });
  }

  if (gallery.posted) {
    meta.push({
      label: "上传时间",
      value: formatPosted(gallery.posted),
    });
  }

  if (gallery.language) {
    meta.push({
      label: "语言",
      value: (
        <>
          {gallery.language}
          {gallery.translated && (
            <span className="ml-1 text-xs text-kumo-success">
              (已翻译)
            </span>
          )}
        </>
      ),
    });
  }

  if (gallery.file_size) {
    meta.push({
      label: "文件大小",
      value: gallery.file_size,
    });
  }

  if (gallery.favorited > 0) {
    meta.push({
      label: "收藏数",
      value: gallery.favorited,
    });
  }

  return (
    <div className="space-y-8">
      {/* Back */}
      <Button
        variant="ghost"
        size="sm"
        onClick={() => navigate(-1)}
        className="w-fit"
      >
        <ArrowLeft className="mr-1 size-4" weight="bold" />
        返回
      </Button>

      {/* Gallery information */}
      <div className="space-y-6">
        {/* Main information */}
        <div className="flex flex-col gap-6 sm:flex-row">
          {/* Cover */}
          <div className="mx-auto w-full max-w-[220px] shrink-0 sm:mx-0 sm:w-[220px]">
            <div className="aspect-[3/4] overflow-hidden rounded-2xl bg-kumo-recessed shadow-lg ring-1 ring-kumo-hairline">
              <img
                src={thumbnailUrl(gallery.cover)}
                alt={gallery.title}
                className="app-image h-full w-full object-cover"
                onError={(e) => {
                  e.currentTarget.style.display = "none";
                  e.currentTarget.nextElementSibling?.classList.remove(
                    "hidden",
                  );
                }}
              />

              <div className="hidden flex h-full items-center justify-center p-4 text-center text-xs text-kumo-subtle">
                图片不可用
              </div>
            </div>
          </div>

          {/* Information */}
          <div className="min-w-0 flex-1 space-y-5">
            {/* Title */}
            <div className="space-y-1.5">
              <h1 className="text-xl font-semibold leading-snug tracking-tight sm:text-2xl lg:text-3xl">
                {gallery.title}
              </h1>

              {gallery.title_jpn && (
                <p className="text-sm text-kumo-subtle sm:text-base">
                  {gallery.title_jpn}
                </p>
              )}
            </div>

            {/* Summary */}
            <div className="flex flex-wrap items-center gap-2.5 text-sm">
              {isFallback && (
                <Chip tone="outline" className="gap-1 text-kumo-subtle">
                  <CloudSlash className="size-3.5" weight="bold" />
                  离线数据
                </Chip>
              )}

              <Chip tone="solid">{gallery.category}</Chip>

              <span className="inline-flex items-center gap-1 text-kumo-default">
                <Star
                  className="size-4 text-kumo-warning"
                  weight="fill"
                />

                <span className="tnum font-medium">
                  {gallery.rating.toFixed(2)}
                </span>

                {gallery.rating_count > 0 && (
                  <span className="text-xs text-kumo-subtle">
                    ({gallery.rating_count})
                  </span>
                )}
              </span>

              <span className="tnum text-kumo-subtle">
                {gallery.page_count} 页
              </span>
            </div>

            {/* Actions */}
            <div className="flex flex-wrap items-center gap-2.5">
              <Button
                variant="primary"
                onClick={() =>
                  navigate(
                    progress?.completed
                      ? `/reader/${id}/${token}?restart=1`
                      : `/reader/${id}/${token}`,
                  )
                }
              >
                <BookOpen
                  className="mr-1.5 size-4"
                  weight="fill"
                />

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
                  <CircleNotch className="mr-1.5 size-4 animate-spin" />
                ) : (
                  <BookmarkSimple
                    className="mr-1.5 size-4"
                    weight={inShelf ? "fill" : "regular"}
                  />
                )}

                {inShelf ? "已收藏" : "收藏"}
              </Button>

              {gallery.page_count > 0 && (
                <Button
                  variant="outline"
                  onClick={() => void handleDownload()}
                  disabled={downloadPending}
                  aria-label="添加下载任务"
                >
                  {downloadPending ? (
                    <CircleNotch className="mr-1.5 size-4 animate-spin" />
                  ) : (
                    <DownloadSimple
                      className="mr-1.5 size-4"
                      weight="bold"
                    />
                  )}

                  下载
                </Button>
              )}
            </div>

            {/* Metadata */}
            {meta.length > 0 && (
              <dl className="grid grid-cols-1 gap-x-8 gap-y-2.5 border-t border-kumo-hairline pt-4 text-sm sm:grid-cols-2">
                {meta.map((entry) => (
                  <div
                    key={entry.label}
                    className="flex min-w-0 items-baseline gap-3"
                  >
                    <dt className="w-16 shrink-0 text-kumo-subtle">
                      {entry.label}
                    </dt>

                    <dd className="min-w-0 break-all font-medium">
                      {entry.value}
                    </dd>
                  </div>
                ))}
              </dl>
            )}
          </div>
        </div>

        {/* Tags */}
        {gallery.tags.length > 0 && (
          <Section title="标签">
            <TagList tags={gallery.tags} />
          </Section>
        )}
      </div>

      {/* Page thumbnails */}
      <Section
        title="页面"
        description={`共 ${gallery.page_count} 页`}
      >
        {pagesQuery.isLoading ? (
          <PageGridSkeleton />
        ) : pagesQuery.data && pagesQuery.data.pages.length > 0 ? (
          <PageThumbnailGrid
            id={id}
            token={token ?? ""}
            pages={pagesQuery.data.pages}
            total={pagesQuery.data.total ?? gallery.page_count}
          />
        ) : (
          <p className="text-sm text-kumo-subtle">
            暂无页面数据
          </p>
        )}
      </Section>
    </div>
  );
}