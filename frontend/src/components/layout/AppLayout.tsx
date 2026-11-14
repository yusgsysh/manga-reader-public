import { Suspense } from "react";
import { Outlet, ScrollRestoration, useLocation } from "react-router";
import { Header } from "./Header";
import { BackToTop } from "../common/BackToTop";
import { PageLoading } from "../common/LoadingScreen";
import { PageTransition } from "../ui";

export function AppLayout() {
  const location = useLocation();

  return (
    <div className="min-h-dvh bg-kumo-base text-kumo-default">
      <ScrollRestoration />
      <Header />
      <main className="app-container pt-3 pb-6 md:pt-4 md:pb-8">
        {/*
          The Suspense boundary sits outside PageTransition so the fallback is
          not affected by the route-enter transform; see PageLoading.
        */}
        <Suspense fallback={<PageLoading />}>
          <PageTransition routeKey={location.pathname}>
            <Outlet />
          </PageTransition>
        </Suspense>
      </main>
      <BackToTop />
    </div>
  );
}
