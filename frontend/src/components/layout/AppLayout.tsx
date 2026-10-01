import { Outlet, ScrollRestoration, useLocation } from "react-router";
import { Header } from "./Header";
import { DesktopSidebar } from "./DesktopSidebar";
import { BackToTop } from "../common/BackToTop";
import { PageTransition } from "../ui";

export function AppLayout() {
  const location = useLocation();

  return (
    <div className="min-h-dvh bg-kumo-base text-kumo-default">
      <ScrollRestoration />
      <DesktopSidebar />
      <Header />
      <div className="lg:pl-[var(--app-sidebar-width)]">
        <main className="app-container py-6 md:py-8">
          <PageTransition routeKey={location.pathname}>
            <Outlet />
          </PageTransition>
        </main>
      </div>
      <BackToTop />
    </div>
  );
}
