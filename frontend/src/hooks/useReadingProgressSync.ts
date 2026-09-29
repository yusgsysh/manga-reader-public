import { useCallback, useEffect, useRef, useState } from "react";
import { updateReadingProgress } from "../api/progress";
import { calculateProgress, clampPageIndex } from "../lib/reader";
import { useUpdateReadingProgress } from "./useReaderData";

const SAVE_DEBOUNCE_MS = 800;

export function useReadingProgressSync(
  id: number,
  token: string,
  total: number,
  initialPage: number,
) {
  const mutation = useUpdateReadingProgress(id, token);
  const [currentPage, setCurrentPage] = useState(initialPage);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const latestPageRef = useRef(initialPage);
  const navigatedRef = useRef(false);
  const totalRef = useRef(total);

  useEffect(() => {
    totalRef.current = total;
  }, [total]);

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
    return { current_page: clamped, progress, completed };
  }, []);

  const onPageChange = useCallback(
    (pageIndex: number) => {
      navigatedRef.current = true;
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
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      updateReadingProgress(id, token, buildBody(latestPageRef.current)).catch(
        () => {
          // Progress save failure must not block reading.
        },
      );
    };
  }, [id, token, buildBody]);

  return {
    currentPage,
    onPageChange,
    isSaving: mutation.isPending,
  };
}
