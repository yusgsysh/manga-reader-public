import { Suspense } from "react";
import { createBrowserRouter } from "react-router";
import { AppLayout } from "../components/layout/AppLayout";
import { RequireAuth } from "../components/auth/RequireAuth";
import {
  BookshelfPage,
  DownloadManagerPage,
  GalleryDetailPage,
  HomePage,
  LazyPage,
  LoginPage,
  PopularPage,
  ReaderLoading,
  ReaderRoute,
  RecentlyReadPage,
  SearchPage,
  SettingsPage,
  SetupPage,
  SubscriptionsPage,
} from "./pages";

export const router = createBrowserRouter([
  {
    path: "/login",
    element: (
      <LazyPage>
        <LoginPage />
      </LazyPage>
    ),
  },
  {
    path: "/setup",
    element: (
      <LazyPage>
        <SetupPage />
      </LazyPage>
    ),
  },
  {
    path: "/",
    element: (
      <RequireAuth>
        <AppLayout />
      </RequireAuth>
    ),
    // Pages are lazy; AppLayout's Suspense boundary handles their fallback so
    // the spinner stays viewport-centered instead of moving with the content.
    children: [
      { index: true, element: <HomePage /> },
      { path: "watched", element: <SubscriptionsPage /> },
      { path: "popular", element: <PopularPage /> },
      { path: "search", element: <SearchPage /> },
      { path: "bookshelf", element: <BookshelfPage /> },
      { path: "recently-read", element: <RecentlyReadPage /> },
      { path: "downloads", element: <DownloadManagerPage /> },
      { path: "settings", element: <SettingsPage /> },
      { path: "gallery/:id/:token", element: <GalleryDetailPage /> },
    ],
  },
  {
    path: "/reader/:id/:token",
    element: (
      <RequireAuth>
        <Suspense fallback={<ReaderLoading />}>
          <ReaderRoute />
        </Suspense>
      </RequireAuth>
    ),
  },
]);
