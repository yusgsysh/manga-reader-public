import type { PrefillJob, PrefillStatus } from "../types/prefill";

const ACTIVE_STATUSES: readonly PrefillStatus[] = ["queued", "running"];
const TERMINAL_STATUSES: readonly PrefillStatus[] = [
  "completed",
  "cancelled",
  "failed",
];

export function isActivePrefillStatus(status: PrefillStatus): boolean {
  return ACTIVE_STATUSES.includes(status);
}

export function isTerminalPrefillStatus(status: PrefillStatus): boolean {
  return TERMINAL_STATUSES.includes(status);
}

export function hasActivePrefillJob(jobs: PrefillJob[]): boolean {
  return jobs.some((job) => isActivePrefillStatus(job.status));
}

export function prefillStatusLabel(status: PrefillStatus): string {
  switch (status) {
    case "queued":
      return "排队中";
    case "running":
      return "下载中";
    case "completed":
      return "已完成";
    case "cancelled":
      return "已取消";
    case "failed":
      return "失败";
    default:
      return status;
  }
}

export function prefillProgressPercent(job: PrefillJob): number {
  if (job.status === "completed") return 100;
  if (!job.progress || job.total <= 0) return 0;
  return Math.min(100, Math.round((job.progress.done / job.total) * 100));
}

export function prefillProgressText(job: PrefillJob): string {
  const progress = job.progress;
  if (!progress) return `${job.total} 页`;
  return `${progress.done} / ${job.total} 页 · 缓存 ${progress.cached} · 抓取 ${progress.fetched}`;
}
