import { useQuery } from "@tanstack/react-query";
import { fetchGallerys, fetchWatched, fetchPopular } from "../api/gallery";

export function useGallerys(page: number) {
  return useQuery({
    queryKey: ["home", page],
    queryFn: () => fetchGallerys(page),
  });
}

export function useWatched(page: number) {
  return useQuery({
    queryKey: ["watched", page],
    queryFn: () => fetchWatched(page),
  });
}

export function usePopular(page: number) {
  return useQuery({
    queryKey: ["popular", page],
    queryFn: () => fetchPopular(page),
  });
}
