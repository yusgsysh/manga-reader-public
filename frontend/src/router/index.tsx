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
      <RequireAuth>
        <Suspense fallback={<ReaderLoading />}>
          <ReaderRoute />
        </Suspense>
      </RequireAuth>
    ),
  },
]);
