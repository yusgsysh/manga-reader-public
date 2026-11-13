import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { Loader } from "@cloudflare/kumo";
import { useAuth } from "../../context/auth";
import { ErrorState } from "../common/ErrorState";

/**
 * Route guard for all application pages. An unauthenticated visitor is sent to
 * /login with the original destination in location state.
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { isAuthenticated, isLoading, error, refresh } = useAuth();
  const location = useLocation();

  if (isLoading) {
    return (
      <div className="flex min-h-dvh items-center justify-center bg-kumo-base">
        <Loader size={32} />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex min-h-dvh items-center justify-center bg-kumo-base px-4">
        <ErrorState
          message="无法连接服务器，请检查后端是否已启动。"
          onRetry={() => void refresh()}
        />
      </div>
    );
  }

  if (!isAuthenticated) {
    return (
      <Navigate
        to="/login"
        replace
        state={{
          from: `${location.pathname}${location.search}${location.hash}`,
        }}
      />
    );
  }

  return <>{children}</>;
}
