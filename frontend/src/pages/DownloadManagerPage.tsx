import { useState } from "react";
import { Link } from "react-router";
import { Badge, Button, Dialog, Input, Loader, Meter, useKumoToastManager } from "@cloudflare/kumo";
import { Download, Loader2, Trash2, X } from "lucide-react";
import {
  useCancelPrefillJob,
  useCleanupPrefillJobs,
  useDeletePrefillJob,
  usePrefillJobs,
} from "../hooks/usePrefillJobs";
import { prefillZipUrl } from "../api/prefill";
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

function downloadZip(id: number) {
  const anchor = document.createElement("a");
  anchor.href = prefillZipUrl(id);
  anchor.rel = "noopener";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

interface JobRowProps {
  job: PrefillJob;
  cancelPending: boolean;
  deletePending: boolean;
  onCancel: (id: number) => void;
  onDelete: (id: number) => void;
}

function JobRow({ job, cancelPending, deletePending, onCancel, onDelete }: JobRowProps) {
  const active = isActivePrefillStatus(job.status);

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
              onClick={() => onCancel(job.id)}
              disabled={cancelPending}
              aria-label="取消任务"
            >
              <X className="mr-1 size-3.5" />
              取消
            </Button>
          ) : (
            <>
              {job.status === "completed" && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => downloadZip(job.id)}
                  aria-label="下载为 ZIP"
                >
                  <Download className="mr-1 size-3.5" />
                  ZIP
                </Button>
              )}
              <Button
                variant="ghost"
                size="sm"
                className="text-kumo-danger"
                onClick={() => onDelete(job.id)}
                disabled={deletePending}
                aria-label="删除记录"
              >
                <Trash2 className="mr-1 size-3.5" />
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
    </div>
  );
}

export function DownloadManagerPage() {
  const { data, isLoading, error, refetch } = usePrefillJobs();
  const cancel = useCancelPrefillJob();
  const remove = useDeletePrefillJob();
  const cleanup = useCleanupPrefillJobs();
  const toast = useKumoToastManager();
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cleanupDays, setCleanupDays] = useState("30");

  const jobs = data?.jobs ?? [];

  const handleCancel = (id: number) => {
    cancel.mutate(id, {
      onSuccess: () =>
        toast.add({ title: "任务已取消", variant: "info" }),
      onError: (err) =>
        toast.add({
          title: "取消任务失败",
          description: err.message,
          variant: "error",
        }),
    });
  };

  const handleDelete = (id: number) => {
    remove.mutate(id, {
      onSuccess: () =>
        toast.add({ title: "记录已删除", variant: "success" }),
      onError: (err) =>
        toast.add({
          title: "删除记录失败",
          description: err.message,
          variant: "error",
        }),
    });
  };

  const handleCleanup = () => {
    const days = Number(cleanupDays);
    if (!Number.isInteger(days) || days < 0) {
      toast.add({ title: "请输入不小于 0 的整数天数", variant: "error" });
      return;
    }
    cleanup.mutate(days, {
      onSuccess: (resp) => {
        toast.add({
          title: "清理完成",
          description: `已删除 ${resp.deleted} 条记录`,
          variant: "success",
        });
        setCleanupOpen(false);
      },
      onError: (err) =>
        toast.add({
          title: "清理失败",
          description: err.message,
          variant: "error",
        }),
    });
  };

  if (isLoading) {
    return (
      <div className="flex justify-center py-20">
        <Loader size={28} />
      </div>
    );
  }

  if (error) {
    return (
      <ErrorState
        message={error.message || "加载下载任务失败"}
        onRetry={() => refetch()}
      />
    );
  }

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-bold">下载管理</h1>
        {jobs.length > 0 && (
          <Button
            variant="ghost"
            size="sm"
            className="text-kumo-danger"
            onClick={() => setCleanupOpen(true)}
          >
            <Trash2 className="mr-1 size-4" />
            清理记录
          </Button>
        )}
      </div>

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
              cancelPending={cancel.isPending && cancel.variables === job.id}
              deletePending={remove.isPending && remove.variables === job.id}
              onCancel={handleCancel}
              onDelete={handleDelete}
            />
          ))}
        </div>
      )}

      <Dialog.Root open={cleanupOpen} onOpenChange={setCleanupOpen}>
        <Dialog className="w-[min(92vw,26rem)] p-6">
          <Dialog.Title className="text-base font-semibold">
            清理下载记录
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-kumo-subtle">
            删除已结束（已完成 / 已取消 / 失败）的任务记录，进行中的任务不受影响。填 0 清空全部已结束记录。
          </Dialog.Description>
          <div className="mt-4">
            <label className="mb-1 block text-xs text-kumo-subtle" htmlFor="cleanup-days">
              保留最近 N 天
            </label>
            <Input
              id="cleanup-days"
              type="number"
              min={0}
              value={cleanupDays}
              onChange={(e) => setCleanupDays(e.target.value)}
            />
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close render={<Button variant="secondary">取消</Button>} />
            <Button
              variant="destructive"
              onClick={handleCleanup}
              disabled={cleanup.isPending}
            >
              {cleanup.isPending && <Loader2 className="mr-1 size-4 animate-spin" />}
              清理
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}
