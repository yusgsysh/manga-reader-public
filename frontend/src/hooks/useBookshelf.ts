import { useQuery } from "@tanstack/react-query";
import { fetchBookshelf } from "../api/bookshelf";

const BOOKSHELF_STALE_TIME = 2 * 60_000;

export function useBookshelf(page: number) {
  return useQuery({
    queryKey: ["bookshelf", page],
    queryFn: () => fetchBookshelf(page),
    staleTime: BOOKSHELF_STALE_TIME,
  });
}
