import { Outlet, ScrollRestoration } from "react-router";
import { Header } from "./Header";
import { BackToTop } from "../common/BackToTop";

export function AppLayout() {
  return (
    <div className="min-h-screen bg-kumo-base text-kumo-default">
      <ScrollRestoration />
      <Header />
      <main className="mx-auto max-w-7xl px-4 py-6">
        <Outlet />
      </main>
      <BackToTop />
    </div>
  );
}
