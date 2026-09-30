import { lazy, Suspense, type ReactNode } from "react";
import { Loader } from "@cloudflare/kumo";

export const HomePage = lazy(() =>
  import("../pages/HomePage").then((m) => ({ default: m.HomePage })),
);
export const SubscriptionsPage = lazy(() =>
  import("../pages/SubscriptionsPage").then((m) => ({
    default: m.SubscriptionsPage,
  })),
);
export const PopularPage = lazy(() =>
  import("../pages/PopularPage").then((m) => ({ default: m.PopularPage })),
);
export const SearchPage = lazy(() =>
  import("../pages/SearchPage").then((m) => ({ default: m.SearchPage })),
);
export const BookshelfPage = lazy(() =>
  import("../pages/BookshelfPage").then((m) => ({ default: m.BookshelfPage })),
);
export const RecentlyReadPage = lazy(() =>
  import("../pages/RecentlyReadPage").then((m) => ({
    default: m.RecentlyReadPage,
  })),
);
export const DownloadManagerPage = lazy(() =>
  import("../pages/DownloadManagerPage").then((m) => ({
    default: m.DownloadManagerPage,
  })),
);
export const GalleryDetailPage = lazy(() =>
  import("../pages/GalleryDetailPage").then((m) => ({
    default: m.GalleryDetailPage,
  })),
);
export const ReaderPage = lazy(() =>
  import("../pages/ReaderPage").then((m) => ({ default: m.ReaderPage })),
);

function PageLoading() {
  return (
    <div className="flex justify-center py-20">
      <Loader size={28} />
    </div>
  );
}

export function LazyPage({ children }: { children: ReactNode }) {
  return <Suspense fallback={<PageLoading />}>{children}</Suspense>;
}

export function ReaderLoading() {
  return (
    <div className="flex h-[100dvh] items-center justify-center bg-kumo-base">
      <Loader size={32} />
    </div>
  );
}
