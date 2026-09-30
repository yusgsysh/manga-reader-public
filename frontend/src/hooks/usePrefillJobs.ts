import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  cancelPrefillJob,
  cleanupPrefillJobs,
  deletePrefillJob,
  fetchPrefillJobs,
  startPrefillJob,
} from "../api/prefill";
import { ApiRequestError } from "../api/client";
import { hasActivePrefillJob } from "../lib/prefill";
import { useKumoToastManager } from "@cloudflare/kumo";
import type { PrefillStartRequest } from "../types/prefill";

const PREFILL_KEY = ["prefill-jobs"] as const;

const PREFILL_STALE_TIME = 30_000;

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
  const toast = useKumoToastManager();
  return useMutation({
    mutationFn: (req: PrefillStartRequest) => startPrefillJob(req),
    onSuccess: (_job, req) => {
      toast.add({
        title: "已添加下载任务",
        description: `共 ${req.urls.length} 页 · 在「下载管理」查看进度`,
        variant: "success",
      });
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
  const toast = useKumoToastManager();
  return useMutation({
    mutationFn: (id: number) => cancelPrefillJob(id),
    onSuccess: () => {
      toast.add({ title: "任务已取消", variant: "info" });
    },
    onError: (err: unknown) => {
      if (err instanceof ApiRequestError && err.status === 409) {
        toast.add({ title: "任务已结束", variant: "info" });
      } else {
        toast.add({
          title: "取消任务失败",
          description: err instanceof Error ? err.message : String(err),
          variant: "error",
        });
      }
    },
    onSettled: () => {
      invalidate();
    },
  });
}

export function useDeletePrefillJob() {
  const invalidate = usePrefillInvalidation();
  const toast = useKumoToastManager();
  return useMutation({
    mutationFn: (id: number) => deletePrefillJob(id),
    onSuccess: () => {
      toast.add({ title: "记录已删除", variant: "success" });
    },
    onError: (err: unknown) => {
      if (err instanceof ApiRequestError && err.status === 409) {
        toast.add({
          title: "任务进行中",
          description: "请先取消任务再删除",
          variant: "error",
        });
      } else {
        toast.add({
          title: "删除记录失败",
          description: err instanceof Error ? err.message : String(err),
          variant: "error",
        });
      }
    },
    onSettled: () => {
      invalidate();
    },
  });
}

export function useCleanupPrefillJobs() {
  const invalidate = usePrefillInvalidation();
  const toast = useKumoToastManager();
  return useMutation({
    mutationFn: (days: number) => cleanupPrefillJobs(days),
    onSuccess: (resp) => {
      toast.add({
        title: "清理完成",
        description: `已删除 ${resp.deleted} 条记录`,
        variant: "success",
      });
    },
    onError: (_err: unknown) => {
      // Error toast handled by component-level onError
    },
    onSettled: () => {
      invalidate();
    },
  });
}