import { Suspense } from "react";
import { createBrowserRouter } from "react-router";
import { AppLayout } from "../components/layout/AppLayout";
import {
  BookshelfPage,
  DownloadManagerPage,
  GalleryDetailPage,
  HomePage,
  LazyPage,
  PopularPage,
  ReaderLoading,
  ReaderPage,
  RecentlyReadPage,
  SearchPage,
  SettingsPage,
  SubscriptionsPage,
} from "./pages";

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
        path: "settings",
        element: (
          <LazyPage>
            <SettingsPage />
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
