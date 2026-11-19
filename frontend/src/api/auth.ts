import { apiGet, apiPostJson } from "./client";

export interface AuthUser {
  /** Local user id (UUID). */
  id: string;
  /** Verified Kinde subject. */
  user_id: string;
  username: string;
  email: string;
  name: string;
  avatar_url: string | null;
  role: string;
  status: string;
  /** "kinde" once the access token carries roles; "local" for legacy tokens. */
  rbac_source?: "kinde" | "local";
  roles?: string[];
  permissions?: string[];
}

/** Current local account for the verified Kinde access token. */
export function fetchAuthUser(): Promise<AuthUser> {
  return apiGet<AuthUser>("/api/auth/me");
}

/**
 * Revokes the local browser bridge cookie and bumps the local session version.
 * Kinde's own logout redirect is performed by the SDK (see AuthContext).
 */
export async function logout(): Promise<void> {
  await apiPostJson("/api/auth/logout");
}
