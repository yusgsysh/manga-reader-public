import { useEffect, useState } from "react";
import { Outlet, ScrollRestoration, useLocation } from "react-router";
import { Header } from "./Header";
import { DesktopSidebar } from "./DesktopSidebar";
import { BackToTop } from "../common/BackToTop";
import { PageTransition } from "../ui";
import { NavigationProvider } from "../../context/NavigationContext";

const SIDEBAR_STORAGE_KEY = "manga-reader-sidebar";

export function AppLayout() {
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem(SIDEBAR_STORAGE_KEY) === "collapsed",
  );

  useEffect(() => {
    localStorage.setItem(
      SIDEBAR_STORAGE_KEY,
      collapsed ? "collapsed" : "expanded",
    );
  }, [collapsed]);

  return (
    <div
      data-sidebar={collapsed ? "collapsed" : "expanded"}
      className="min-h-dvh bg-kumo-base text-kumo-default"
    >
      <ScrollRestoration />
      <DesktopSidebar
        collapsed={collapsed}
        onToggle={() => setCollapsed((value) => !value)}
      />
      <Header />
      <div className="transition-[padding] duration-200 lg:pl-[var(--app-sidebar-width)]">
        <main className="app-container py-6 md:py-8">
          <NavigationProvider>
            <PageTransition routeKey={location.pathname}>
              <Outlet />
            </PageTransition>
          </NavigationProvider>
        </main>
      </div>
      <BackToTop />
    </div>
  );
}
