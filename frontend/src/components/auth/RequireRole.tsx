import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { useAuth } from "../../context/auth";

/**
 * UI-level role gate. The backend enforces the same roles independently; this
 * only keeps ordinary users out of admin screens.
 */
export function RequireRole({
  role,
  children,
}: {
  role: "platform_admin" | "tenant_admin";
  children: ReactNode;
}) {
  const { isPlatformAdmin, isTenantAdmin } = useAuth();
  const allowed =
    role === "platform_admin" ? isPlatformAdmin : isTenantAdmin;
  if (!allowed) return <Navigate to="/" replace />;
  return <>{children}</>;
}
