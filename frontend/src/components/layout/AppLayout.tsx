import { useEffect, useState } from "react";
import { Outlet, ScrollRestoration, useLocation } from "react-router";
import { Header } from "./Header";
import { DesktopSidebar } from "./DesktopSidebar";
import { BackToTop } from "../common/BackToTop";
import { PageTransition } from "../ui";

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
      {/*
        Content padding snaps to the sidebar width instead of transitioning:
        animating padding re-lays-out the whole main subtree every frame, which
        is what dropped frames on the toggle. The (cheap, fixed-position)
        aside keeps its width animation, and it covers the snapped content
        while it collapses, so the switch still reads as one motion.
      */}
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
