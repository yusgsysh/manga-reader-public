import { Suspense } from "react";
import { createBrowserRouter } from "react-router";
import { AppLayout } from "../components/layout/AppLayout";
import { RequireAuth } from "../components/auth/RequireAuth";
import { RequireRole } from "../components/auth/RequireRole";
import {
  BookshelfPage,
  DownloadManagerPage,
  GalleryDetailPage,
  HomePage,
  LazyPage,
  LoginPage,
  PlatformAdminPage,
  NodeAdminPage,
  PopularPage,
  ReaderLoading,
  ReaderRoute,
  RecentlyReadPage,
  SearchPage,
  SettingsPage,
  SubscriptionsPage,
  TagSettingsPage,
  TenantAdminPage,
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
        path: "admin/tenants",
        element: (
          <RequireRole role="platform_admin">
            <LazyPage>
              <PlatformAdminPage />
            </LazyPage>
          </RequireRole>
        ),
      },
      {
        path: "admin/nodes",
        element: (
          <RequireRole role="platform_admin">
            <LazyPage>
              <NodeAdminPage />
            </LazyPage>
          </RequireRole>
        ),
      },
      {
        path: "tenant/users",
        element: (
          <RequireRole role="tenant_admin">
            <LazyPage>
              <TenantAdminPage />
            </LazyPage>
          </RequireRole>
        ),
      },
      {
        path: "tag-settings",
        element: (
          <LazyPage>
            <TagSettingsPage />
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
