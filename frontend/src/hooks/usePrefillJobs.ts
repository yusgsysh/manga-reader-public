import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  cancelPrefillJob,
  cleanupPrefillJobs,
  deletePrefillJob,
  fetchPrefillJobs,
  startPrefillJob,
} from "../api/prefill";
import { ApiRequestError } from "../api/client";
import { PREFILL_STALE_TIME } from "../lib/cacheConfig";
import { hasActivePrefillJob } from "../lib/prefill";
import { useToast } from "../components/ui";
import type { PrefillStartRequest } from "../types/prefill";

const PREFILL_KEY = ["prefill-jobs"] as const;

export function usePrefillJobs() {
  return useQuery({
    queryKey: PREFILL_KEY,
    queryFn: fetchPrefillJobs,
    staleTime: PREFILL_STALE_TIME,
    // Poll while any job is queued or running; stop when everything settles.
    refetchInterval: (query) =>
      hasActivePrefillJob(query.state.data?.jobs ?? []) ? 1500 : false,
    // Refetch on window focus to pick up tasks created in other tabs.
    refetchOnWindowFocus: true,
  });
}

function usePrefillInvalidation() {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: PREFILL_KEY });
}

export function useStartPrefillJob() {
  const invalidate = usePrefillInvalidation();
  const toast = useToast();
  return useMutation({
    mutationFn: (req: PrefillStartRequest) => startPrefillJob(req),
    onSuccess: (_job, req) => {
      toast.success(
        "已添加下载任务",
        `共 ${req.urls.length} 页 · 在「下载管理」查看进度`,
      );
    },
    onError: (_err: unknown) => {
      // Error toast handled by component-level onError
    },
    onSettled: () => {
      invalidate();
    },
  });
}

export function useCancelPrefillJob() {
  const invalidate = usePrefillInvalidation();
  const toast = useToast();
  return useMutation({
    mutationFn: (id: string) => cancelPrefillJob(id),
    onSuccess: () => {
      toast.info("任务已取消");
    },
    onError: (err: unknown) => {
      if (err instanceof ApiRequestError && err.status === 409) {
        toast.info("任务已结束");
      } else {
        toast.error("取消任务失败", err);
      }
    },
    onSettled: () => {
      invalidate();
    },
  });
}

export function useDeletePrefillJob() {
  const invalidate = usePrefillInvalidation();
  const toast = useToast();
  return useMutation({
    mutationFn: (id: string) => deletePrefillJob(id),
    onSuccess: () => {
      toast.success("记录已删除");
    },
    onError: (err: unknown) => {
      if (err instanceof ApiRequestError && err.status === 409) {
        toast.error("任务进行中", "请先取消任务再删除");
      } else {
        toast.error("删除记录失败", err);
      }
    },
    onSettled: () => {
      invalidate();
    },
  });
}

export function useCleanupPrefillJobs() {
  const invalidate = usePrefillInvalidation();
  const toast = useToast();
  return useMutation({
    mutationFn: (days: number) => cleanupPrefillJobs(days),
    onSuccess: (resp) => {
      toast.success("清理完成", `已删除 ${resp.deleted} 条记录`);
    },
    onError: (_err: unknown) => {
      // Error toast handled by component-level onError
    },
    onSettled: () => {
      invalidate();
    },
  });
}