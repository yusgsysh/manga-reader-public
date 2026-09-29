import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { updateReadingProgress } from "../api/progress";
import { calculateProgress, clampPageIndex } from "../lib/reader";
import { trackProgressSave } from "../lib/progressSave";
import type { ProgressMetadata, ReadingProgress } from "../types/reader";
import {
  applyReadingProgressToCaches,
  invalidateReadingLists,
  useUpdateReadingProgress,
} from "./useReaderData";

const SAVE_DEBOUNCE_MS = 800;

export function useReadingProgressSync(
  id: number,
  token: string,
  total: number,
  initialPage: number,
  metadata?: ProgressMetadata,
) {
  const mutation = useUpdateReadingProgress(id, token);
  const queryClient = useQueryClient();
  const [currentPage, setCurrentPage] = useState(initialPage);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const latestPageRef = useRef(initialPage);
  const navigatedRef = useRef(false);
  const flushedRef = useRef(false);
  const totalRef = useRef(total);
  const metadataRef = useRef(metadata);

  useEffect(() => {
    totalRef.current = total;
  }, [total]);

  useEffect(() => {
    metadataRef.current = metadata;
  }, [metadata]);

  // Keep the restored page in sync while the reader data is still loading.
  useEffect(() => {
    if (!navigatedRef.current) {
      latestPageRef.current = initialPage;
      setCurrentPage(initialPage);
    }
  }, [initialPage]);

  const buildBody = useCallback((page: number) => {
    const clamped = clampPageIndex(page, totalRef.current);
    const { progress, completed } = calculateProgress(
      clamped,
      totalRef.current,
    );
    return {
      current_page: clamped,
      progress,
      completed,
      ...metadataRef.current,
    };
  }, []);

  const flushProgress = useCallback(() => {
    if (flushedRef.current) return;
    flushedRef.current = true;
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }

    const body = buildBody(latestPageRef.current);
    const previous = queryClient.getQueryData<ReadingProgress>([
      "reading-progress",
      id,
      token,
    ]);

    applyReadingProgressToCaches(queryClient, id, token, {
      gallery_id: previous?.gallery_id ?? id,
      token: previous?.token ?? token,
      current_page: body.current_page,
      progress: body.progress,
      completed: body.completed,
      started_at: previous?.started_at ?? null,
      updated_at: new Date().toISOString(),
    });

    trackProgressSave(
      updateReadingProgress(id, token, body)
        .then((data) => {
          applyReadingProgressToCaches(queryClient, id, token, data);
          invalidateReadingLists(queryClient);
        })
        .catch(() => {
          invalidateReadingLists(queryClient);
        }),
    );
  }, [buildBody, queryClient, id, token]);

  const onPageChange = useCallback(
    (pageIndex: number) => {
      navigatedRef.current = true;
      flushedRef.current = false;
      latestPageRef.current = pageIndex;
      setCurrentPage(pageIndex);
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        mutation.mutate(buildBody(latestPageRef.current));
      }, SAVE_DEBOUNCE_MS);
    },
    [mutation, buildBody],
  );

  // Flush the latest progress when leaving the reader.
  useEffect(() => {
    return () => {
      flushProgress();
    };
  }, [flushProgress]);

  return {
    currentPage,
    onPageChange,
    isSaving: mutation.isPending,
    flushProgress,
  };
}
