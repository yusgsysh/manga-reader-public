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
        Content slides with the rail. The padding transition reflows the main
        subtree each frame, so every frame must stay cheap: cards already opt
        into `content-visibility: auto` (off-screen ones skip layout), and this
        mirrors the aside's 200ms ease-out so both edges move as one motion.
        When collapsed, the extra inline inset reserves the corner the expand
        button floats in; padding both sides keeps the page centered.
      */}
      <div className="transition-[padding] duration-200 ease-out lg:pl-[calc(var(--app-sidebar-width)+var(--app-content-inset))] lg:pr-[var(--app-content-inset)]">
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
