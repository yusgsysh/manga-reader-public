import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { useAuth } from "../../context/auth";

/**
 * UI-level role gate. The backend enforces the same role independently; this
 * only keeps ordinary users out of admin screens.
 */
export function RequireRole({
  role,
  children,
}: {
  role: "admin";
  children: ReactNode;
}) {
  const { isAdmin } = useAuth();
  if (role !== "admin" || !isAdmin) return <Navigate to="/" replace />;
  return <>{children}</>;
}
