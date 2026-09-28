import { lazy, Suspense, type ReactNode } from "react";
import { createBrowserRouter } from "react-router";
import { Loader } from "@cloudflare/kumo";
import { AppLayout } from "../components/layout/AppLayout";

const HomePage = lazy(() =>
  import("../pages/HomePage").then((m) => ({ default: m.HomePage })),
);
const SubscriptionsPage = lazy(() =>
  import("../pages/SubscriptionsPage").then((m) => ({
    default: m.SubscriptionsPage,
  })),
);
const PopularPage = lazy(() =>
  import("../pages/PopularPage").then((m) => ({ default: m.PopularPage })),
);
const SearchPage = lazy(() =>
  import("../pages/SearchPage").then((m) => ({ default: m.SearchPage })),
);
const BookshelfPage = lazy(() =>
  import("../pages/BookshelfPage").then((m) => ({ default: m.BookshelfPage })),
);
const RecentlyReadPage = lazy(() =>
  import("../pages/RecentlyReadPage").then((m) => ({
    default: m.RecentlyReadPage,
  })),
);
const DownloadManagerPage = lazy(() =>
  import("../pages/DownloadManagerPage").then((m) => ({
    default: m.DownloadManagerPage,
  })),
);
const GalleryDetailPage = lazy(() =>
  import("../pages/GalleryDetailPage").then((m) => ({
    default: m.GalleryDetailPage,
  })),
);
const ReaderPage = lazy(() =>
  import("../pages/ReaderPage").then((m) => ({ default: m.ReaderPage })),
);

function PageLoading() {
  return (
    <div className="flex justify-center py-20">
      <Loader size={28} />
    </div>
  );
}

function LazyPage({ children }: { children: ReactNode }) {
  return <Suspense fallback={<PageLoading />}>{children}</Suspense>;
}

function ReaderLoading() {
  return (
    <div className="flex h-[100dvh] items-center justify-center bg-kumo-base">
      <Loader size={32} />
    </div>
  );
}

export const router = createBrowserRouter([
  {
    path: "/",
    element: <AppLayout />,
    children: [
      {
        index: true,
        element: (
          <LazyPage>
            <HomePage />
          </LazyPage>
        ),
      },
      {
        path: "watched",
        element: (
          <LazyPage>
            <SubscriptionsPage />
          </LazyPage>
        ),
      },
      {
        path: "popular",
        element: (
          <LazyPage>
            <PopularPage />
          </LazyPage>
        ),
      },
      {
        path: "search",
        element: (
          <LazyPage>
            <SearchPage />
          </LazyPage>
        ),
      },
      {
        path: "bookshelf",
        element: (
          <LazyPage>
            <BookshelfPage />
          </LazyPage>
        ),
      },
      {
        path: "recently-read",
        element: (
          <LazyPage>
            <RecentlyReadPage />
          </LazyPage>
        ),
      },
      {
        path: "downloads",
        element: (
          <LazyPage>
            <DownloadManagerPage />
          </LazyPage>
        ),
      },
      {
        path: "gallery/:id/:token",
        element: (
          <LazyPage>
            <GalleryDetailPage />
          </LazyPage>
        ),
      },
    ],
  },
  {
    path: "/reader/:id/:token",
    element: (
      <Suspense fallback={<ReaderLoading />}>
        <ReaderPage />
      </Suspense>
    ),
  },
]);
