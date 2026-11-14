import { lazy, Suspense, type ReactNode } from "react";
import { useParams } from "react-router";
import { Loader } from "@cloudflare/kumo";
import { LoadingScreen } from "../components/common/LoadingScreen";

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
const ReaderPage = lazy(() =>
  import("../pages/ReaderPage").then((m) => ({ default: m.ReaderPage })),
);
export const SettingsPage = lazy(() =>
  import("../pages/SettingsPage").then((m) => ({ default: m.SettingsPage })),
);
export const LoginPage = lazy(() =>
  import("../pages/LoginPage").then((m) => ({ default: m.LoginPage })),
);
export const SetupPage = lazy(() =>
  import("../pages/SetupPage").then((m) => ({ default: m.SetupPage })),
);

function PageLoading() {
  return (
    <div className="flex justify-center py-20">
      <Loader size={28} />
    </div>
  );
}

export function LazyPage({
  children,
  fullscreen = false,
}: {
  children: ReactNode;
  /** Use the viewport-centered loader for standalone routes (login, setup). */
  fullscreen?: boolean;
}) {
  return (
    <Suspense fallback={fullscreen ? <LoadingScreen /> : <PageLoading />}>
      {children}
    </Suspense>
  );
}

export function ReaderLoading() {
  return <LoadingScreen message="正在加载漫画…" />;
}

// Remount the reader when the gallery changes so all reader state (frozen page
// snapshot, resume target, fullscreen) resets cleanly.
export function ReaderRoute() {
  const { id, token } = useParams<{ id: string; token: string }>();
  return <ReaderPage key={`${id}:${token}`} />;
}
