import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  cancelPrefillJob,
  cleanupPrefillJobs,
  deletePrefillJob,
  fetchPrefillJobs,
  startPrefillJob,
} from "../api/prefill";
import { hasActivePrefillJob } from "../lib/prefill";
import type { PrefillStartRequest } from "../types/prefill";

const PREFILL_KEY = ["prefill-jobs"] as const;

export function usePrefillJobs() {
  return useQuery({
    queryKey: PREFILL_KEY,
    queryFn: fetchPrefillJobs,
    // Poll while any job is queued or running; stop when everything settles.
    refetchInterval: (query) =>
      hasActivePrefillJob(query.state.data?.jobs ?? []) ? 1500 : false,
  });
}

function usePrefillInvalidation() {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: PREFILL_KEY });
}

export function useStartPrefillJob() {
  const invalidate = usePrefillInvalidation();
  return useMutation({
    mutationFn: (req: PrefillStartRequest) => startPrefillJob(req),
    onSuccess: invalidate,
  });
}

export function useCancelPrefillJob() {
  const invalidate = usePrefillInvalidation();
  return useMutation({
    mutationFn: (id: number) => cancelPrefillJob(id),
    onSuccess: invalidate,
  });
}

export function useDeletePrefillJob() {
  const invalidate = usePrefillInvalidation();
  return useMutation({
    mutationFn: (id: number) => deletePrefillJob(id),
    onSuccess: invalidate,
  });
}

export function useCleanupPrefillJobs() {
  const invalidate = usePrefillInvalidation();
  return useMutation({
    mutationFn: (days: number) => cleanupPrefillJobs(days),
    onSuccess: invalidate,
  });
}
