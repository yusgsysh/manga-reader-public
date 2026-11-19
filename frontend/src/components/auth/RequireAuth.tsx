import { useEffect, type ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import {
  clearReturnPath,
  readReturnPath,
  useAuth,
} from "../../context/auth";
import { ErrorState } from "../common/ErrorState";
import { LoadingScreen } from "../common/LoadingScreen";

/**
 * Route guard for all application pages. An unauthenticated visitor is sent to
 * /login with the original destination in location state. After a Kinde
 * callback, the destination saved before the redirect is restored once.
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { isAuthenticated, isLoading, error, refresh } = useAuth();
  const location = useLocation();
  const current = `${location.pathname}${location.search}${location.hash}`;
  const pending = isAuthenticated ? readReturnPath() : null;

  useEffect(() => {
    if (isAuthenticated && pending === current) {
      clearReturnPath();
    }
  }, [isAuthenticated, pending, current]);

  if (isLoading) {
    return <LoadingScreen />;
  }

  if (error) {
    return (
      <div className="flex min-h-dvh items-center justify-center bg-kumo-base px-4">
        <ErrorState
          message={error.message || "无法连接服务器，请检查后端是否已启动。"}
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
        state={{ from: current }}
      />
    );
  }

  if (pending && pending !== current) {
    return <Navigate to={pending} replace />;
  }

  return <>{children}</>;
}
