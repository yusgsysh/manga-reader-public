import { Outlet, ScrollRestoration } from "react-router";
import { Header } from "./Header";
import { BackToTop } from "../common/BackToTop";

export function AppLayout() {
  return (
    <div className="min-h-dvh bg-kumo-base text-kumo-default">
      <ScrollRestoration />
      <Header />
      <main className="app-container py-6">
        <Outlet />
      </main>
      <BackToTop />
    </div>
  );
}
