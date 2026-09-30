import { useState } from "react";
import { Link } from "react-router";
import { Badge, Button, Dialog, Loader, Meter } from "@cloudflare/kumo";
import {
  DownloadSimple,
  CircleNotch,
  Trash,
  X,
  WarningCircle,
} from "@phosphor-icons/react";
import {
  useCancelPrefillJob,
  useCleanupPrefillJobs,
  useDeletePrefillJob,
  usePrefillJobs,
} from "../hooks/usePrefillJobs";
import { prefillZipUrl, headPrefillZip } from "../api/prefill";
import {
  isActivePrefillStatus,
  prefillProgressPercent,
  prefillProgressText,
  prefillStatusLabel,
} from "../lib/prefill";
import { formatRelativeTime } from "../lib/time";
import { EmptyState } from "../components/common/EmptyState";
import { ErrorState } from "../components/common/ErrorState";
import type { PrefillJob, PrefillStatus } from "../types/prefill";

function statusBadgeVariant(
  status: PrefillStatus,
): "secondary" | "info" | "success" | "neutral" | "error" {
  switch (status) {
    case "queued":
      return "secondary";
    case "running":
      return "info";
    case "completed":
      return "success";
    case "cancelled":
      return "neutral";
    case "failed":
      return "error";
  }
}

interface JobRowProps {
  job: PrefillJob;
  zippingIds: Set<number>;
  onZipStart: (id: number) => void;
}

function JobRow({
  job,
  zippingIds,
  onZipStart,
}: JobRowProps) {
  const active = isActivePrefillStatus(job.status);
  const isZipping = zippingIds.has(job.id);
  const cancel = useCancelPrefillJob();
  const remove = useDeletePrefillJob();
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);

  const handleDelete = () => {
    setDeleteConfirmOpen(true);
  };

  const confirmDelete = () => {
    remove.mutate(job.id);
    setDeleteConfirmOpen(false);
  };

  return (
    <div className="space-y-3 rounded-lg border border-kumo-hairline bg-kumo-elevated p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-sm font-semibold">
              {job.title || "未命名任务"}
            </h2>
            <Badge variant={statusBadgeVariant(job.status)}>
              {prefillStatusLabel(job.status)}
            </Badge>
            {job.failed_count > 0 && (
              <span className="text-xs text-kumo-danger">
                失败 {job.failed_count} 页
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-kumo-subtle">
            共 {job.total} 页 · 创建于 {formatRelativeTime(job.created_at)}
            {job.finished_at && (
              <> · 结束于 {formatRelativeTime(job.finished_at)}</>
            )}
          </p>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {active ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => cancel.mutate(job.id)}
              disabled={cancel.isPending}
              aria-label="取消任务"
            >
              <X className="mr-1 size-3.5" weight="bold" />
              取消
            </Button>
          ) : (
            <>
              {job.status === "completed" && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => onZipStart(job.id)}
                  disabled={isZipping}
                  aria-label="下载为 ZIP"
                >
                  {isZipping ? (
                    <CircleNotch className="mr-1 size-3.5 animate-spin" />
                  ) : (
                    <DownloadSimple className="mr-1 size-3.5" weight="bold" />
                  )}
                  ZIP
                </Button>
              )}
              <Button
                variant="ghost"
                size="sm"
                className="text-kumo-danger"
                onClick={handleDelete}
                disabled={remove.isPending}
                aria-label="删除记录"
              >
                <Trash className="mr-1 size-3.5" weight="bold" />
                删除
              </Button>
            </>
          )}
        </div>
      </div>

      {active && (
        <Meter
          label="下载进度"
          value={prefillProgressPercent(job)}
          customValue={prefillProgressText(job)}
          showValue
        />
      )}

      {job.failed_count > 0 && !active && (
        <p className="text-xs text-kumo-danger flex items-center gap-1">
          <WarningCircle className="size-3.5" weight="fill" />
          下载完成，但有 {job.failed_count} 页缺失（已在 ZIP 中标记为 _missing.txt）
        </p>
      )}

      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-kumo-subtle">
        {job.gallery_id !== null && job.gallery_token && (
          <Link
            to={`/gallery/${job.gallery_id}/${job.gallery_token}`}
            className="hover:underline"
          >
            打开画廊
          </Link>
        )}
        {job.errors.length > 0 && (
          <span className="text-kumo-danger">
            {job.errors
              .slice(0, 2)
              .map((e) => `第 ${e.index + 1} 页：${e.error}`)
              .join("；")}
            {job.failed_count > 2 && `等 ${job.failed_count} 个错误`}
          </span>
        )}
      </div>

      <Dialog.Root open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
        <Dialog className="w-[min(92vw,26rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            删除记录
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            确定要删除 "{job.title || "未命名任务"}" 的记录吗？此操作不可恢复。
          </Dialog.Description>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="secondary">取消</Button>} />
            <Button
              variant="destructive"
              onClick={confirmDelete}
              disabled={remove.isPending}
            >
              {remove.isPending && <CircleNotch className="mr-1 size-4 animate-spin" />}
              删除
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export function DownloadManagerPage() {
  const { data, isLoading, error, refetch } = usePrefillJobs();
  const cleanup = useCleanupPrefillJobs();
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [zippingIds, setZippingIds] = useState<Set<number>>(new Set());

  const jobs = data?.jobs ?? [];

  const handleCleanup = () => {
    cleanup.mutate(0);
  };

  const onZipStart = (id: number) => {
    setZippingIds((prev) => new Set(prev).add(id));
    headPrefillZip(id)
      .then((ok) => {
        if (!ok) {
          throw new Error("无法下载：任务不存在或缓存未就绪");
        }
        const anchor = document.createElement("a");
        anchor.href = prefillZipUrl(id);
        anchor.target = "_blank";
        anchor.rel = "noopener";
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
      })
      .catch((err) => {
        // Error toast is handled by the component's error boundary or could be added here
        console.error("Download failed:", err);
      })
      .finally(() => {
        setZippingIds((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
      });
  };

  if (isLoading) {
    return (
      <div className="flex justify-center py-20">
        <Loader size={28} />
      </div>
    );
  }

  if (error && !data) {
    return (
      <ErrorState
        message={error.message || "加载下载任务失败"}
        onRetry={() => refetch()}
      />
    );
  }

  return (
    <div>
      {/* Error banner for transient failures while data is still visible */}
      {error && data && (
        <div className="mb-4 flex items-center gap-2 rounded-md bg-kumo-danger/10 p-3 text-sm text-kumo-danger">
          <WarningCircle className="size-4 shrink-0" weight="fill" />
          <span>刷新失败，正在重试… ({error.message})</span>
          <Button variant="ghost" size="sm" onClick={() => refetch()}>
            立即重试
          </Button>
        </div>
      )}

      {jobs.length > 0 && (
        <div className="mb-4 flex items-center justify-end">
          <Button
            variant="ghost"
            size="sm"
            className="text-kumo-danger"
            onClick={() => setCleanupOpen(true)}
          >
            <Trash className="mr-1 size-4" weight="bold" />
            清理记录
          </Button>
        </div>
      )}

      {jobs.length === 0 ? (
        <EmptyState
          message="暂无下载任务"
          actionLabel="浏览首页"
          actionTo="/"
        />
      ) : (
        <div className="space-y-3">
          {jobs.map((job) => (
            <JobRow
              key={job.id}
              job={job}
              zippingIds={zippingIds}
              onZipStart={onZipStart}
            />
          ))}
        </div>
      )}

      {jobs.length >= 200 && (
        <p className="mt-4 text-center text-xs text-kumo-subtle">
          仅显示最近 200 条记录
        </p>
      )}

      <Dialog.Root open={cleanupOpen} onOpenChange={setCleanupOpen}>
        <Dialog className="w-[min(92vw,26rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            清理全部下载记录
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            清空所有已结束（已完成 / 已取消 / 失败）的任务记录，进行中的任务不受影响。
          </Dialog.Description>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="secondary">取消</Button>} />
            <Button
              variant="destructive"
              onClick={handleCleanup}
              disabled={cleanup.isPending}
            >
              {cleanup.isPending && <CircleNotch className="mr-1 size-4 animate-spin" />}
              清理
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}