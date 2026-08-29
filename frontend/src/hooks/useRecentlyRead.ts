import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { fetchRecentlyRead } from "../api/recentlyRead";
import { fetchGallery } from "../api/gallery";
import type { Gallery } from "../types/reader";
import type { RecentlyReadItem } from "../types/recentlyRead";

function needsEnrichment(item: RecentlyReadItem): boolean {
  return !item.title || !item.thumbnail || !item.pages;
}

export function useRecentlyRead() {
  const listQuery = useQuery({
    queryKey: ["recently-read"],
    queryFn: fetchRecentlyRead,
  });

  const items = useMemo(() => listQuery.data?.results ?? [], [listQuery.data]);
  const missing = useMemo(() => items.filter(needsEnrichment), [items]);

  // Galleries removed from the bookshelf lack title/thumbnail in the
  // recently-read response. Fetch their metadata and merge it back.
  const galleryQueries = useQueries({
    queries: missing.map((item) => ({
      queryKey: ["gallery", item.id, item.token],
      queryFn: () => fetchGallery(item.id, item.token),
      enabled: listQuery.isSuccess,
    })),
  });

  const data = useMemo(() => {
    if (!listQuery.data) return listQuery.data;
    const lookup = new Map<number, Gallery>();
    missing.forEach((item, index) => {
      const gallery = galleryQueries[index]?.data;
      if (gallery) lookup.set(item.id, gallery);
    });

    return {
      ...listQuery.data,
      results: items.map((item) => {
        const gallery = lookup.get(item.id);
        if (!gallery) return item;
        return {
          ...item,
          title: item.title || gallery.title,
          title_jpn: item.title_jpn || gallery.title_jpn,
          thumbnail: item.thumbnail || gallery.thumbnail,
          pages: item.pages || gallery.page_count,
          category: (item.category || gallery.category) as RecentlyReadItem["category"],
        };
      }),
    };
  }, [listQuery.data, items, missing, galleryQueries]);

  return { ...listQuery, data };
}
