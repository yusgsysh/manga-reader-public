import { useQuery } from "@tanstack/react-query";
import { fetchBookshelf } from "../api/bookshelf";

export function useBookshelf(page: number) {
  return useQuery({
    queryKey: ["bookshelf", page],
    queryFn: () => fetchBookshelf(page),
  });
}
