import { Suspense } from "react";
import { Outlet, ScrollRestoration, useLocation } from "react-router";
import { Header } from "./Header";
import { BackToTop } from "../common/BackToTop";
import { PageSkeleton } from "./PageSkeleton";
import { PageTransition } from "../ui";

export function AppLayout() {
  const location = useLocation();

  return (
    <div className="min-h-dvh bg-kumo-base text-kumo-default">
      <ScrollRestoration />
      <Header />
      <main className="app-container pt-3 pb-6 md:pt-4 md:pb-8">
        {/*
          This boundary covers the route's lazy chunk. Its fallback is a
          layout-shaped skeleton (not a full-screen spinner) so a refresh
          paints the header plus skeleton cards immediately; the boundary sits
          outside PageTransition so the skeleton is not affected by the
          route-enter transform.
        */}
        <Suspense fallback={<PageSkeleton />}>
          <PageTransition routeKey={location.pathname}>
            <Outlet />
          </PageTransition>
        </Suspense>
      </main>
      <BackToTop />
    </div>
  );
}
