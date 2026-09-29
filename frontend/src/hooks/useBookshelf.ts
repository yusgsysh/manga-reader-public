import { useInfiniteQuery } from "@tanstack/react-query";
import { fetchBookshelf } from "../api/bookshelf";
import { whenProgressSavesSettled } from "../lib/progressSave";

const BOOKSHELF_STALE_TIME = 2 * 60_000;

export function useBookshelf() {
  return useInfiniteQuery({
    queryKey: ["bookshelf"],
    queryFn: async ({ pageParam }) => {
      await whenProgressSavesSettled();
      return fetchBookshelf(pageParam);
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage) =>
      (lastPage.page + 1) * lastPage.page_size < lastPage.total
        ? lastPage.page + 1
        : undefined,
    staleTime: BOOKSHELF_STALE_TIME,
  });
}
