import { useCallback, useEffect, useReducer, useRef } from "react";
import { updateReadingProgress } from "../api/progress";
import { calculateProgress, clampPageIndex } from "../lib/reader";
import { useUpdateReadingProgress } from "./useReaderData";

const SAVE_DEBOUNCE_MS = 800;

type ReaderState = {
  currentPage: number;
  latestPage: number;
  navigated: boolean;
  total: number;
};

type ReaderAction =
  | { type: "SET_TOTAL"; total: number }
  | { type: "SET_INITIAL_PAGE"; page: number }
  | { type: "NAVIGATE_TO"; page: number };

function readerReducer(state: ReaderState, action: ReaderAction): ReaderState {
  switch (action.type) {
    case "SET_TOTAL":
      return { ...state, total: action.total };
    case "SET_INITIAL_PAGE":
      if (state.navigated) return state;
      return { ...state, currentPage: action.page, latestPage: action.page };
    case "NAVIGATE_TO":
      return {
        ...state,
        currentPage: action.page,
        latestPage: action.page,
        navigated: true,
      };
  }
}

export function useReadingProgressSync(
  id: number,
  token: string,
  total: number,
  initialPage: number,
) {
  const mutation = useUpdateReadingProgress(id, token);
  const [state, dispatch] = useReducer(readerReducer, {
    currentPage: initialPage,
    latestPage: initialPage,
    navigated: false,
    total,
  });
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    dispatch({ type: "SET_TOTAL", total });
  }, [total]);

  useEffect(() => {
    dispatch({ type: "SET_INITIAL_PAGE", page: initialPage });
  }, [initialPage]);

  const buildBody = useCallback(
    (page: number) => {
      const clamped = clampPageIndex(page, state.total);
      const { progress, completed } = calculateProgress(clamped, state.total);
      return { current_page: clamped, progress, completed };
    },
    [state.total],
  );

  const onPageChange = useCallback(
    (pageIndex: number) => {
      dispatch({ type: "NAVIGATE_TO", page: pageIndex });
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        mutation.mutate(buildBody(state.latestPage));
      }, SAVE_DEBOUNCE_MS);
    },
    [mutation, buildBody, state.latestPage],
  );

  useEffect(() => {
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      updateReadingProgress(id, token, buildBody(state.latestPage)).catch(
        () => {
          // Progress save failure must not block reading.
        },
      );
    };
  }, [id, token, buildBody, state.latestPage]);

  return {
    currentPage: state.currentPage,
    onPageChange,
    isSaving: mutation.isPending,
  };
}
