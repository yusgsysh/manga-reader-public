import { apiGet, apiPostJson, ApiRequestError, setAccessToken } from "./client";

export interface AuthTenant {
  id: number;
  name: string;
  slug: string;
  status: string;
}

export interface AuthUser {
  id: number;
  tenant_id: number;
  username: string;
  role: string;
}

export interface AuthIdentity {
  user: AuthUser;
  tenant: AuthTenant;
}

export interface AuthStatus {
  enabled: boolean;
  initialized: boolean;
}

export interface AuthSession extends AuthStatus {
  user: AuthUser | null;
  tenant: AuthTenant | null;
}

interface LoginResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
}

interface MeResponse {
  id: number;
  tenant_id: number;
  username: string;
  role: string;
  tenant: AuthTenant;
}

export function fetchAuthStatus(): Promise<AuthStatus> {
  return apiGet<AuthStatus>("/api/auth/status");
}

export async function fetchCurrentUser(): Promise<AuthIdentity> {
  const response = await apiGet<MeResponse>("/api/auth/me");
  return {
    user: {
      id: response.id,
      tenant_id: response.tenant_id,
      username: response.username,
      role: response.role,
    },
    tenant: response.tenant,
  };
}

/**
 * Resolves the complete auth state used by the SPA.
 *
 * The JWT is carried by an HttpOnly cookie after login, so a page refresh only
 * needs /me to restore the session. A 401 simply means "no session yet".
 */
export async function fetchAuthSession(): Promise<AuthSession> {
  const status = await fetchAuthStatus();
  if (!status.enabled) {
    return { ...status, user: null, tenant: null };
  }
  try {
    const identity = await fetchCurrentUser();
    return { ...status, ...identity };
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 401) {
      return { ...status, user: null, tenant: null };
    }
    throw error;
  }
}

export async function login(
  tenant: string,
  username: string,
  password: string,
): Promise<AuthIdentity> {
  const result = await apiPostJson<LoginResponse>("/api/auth/login", {
    tenant,
    username,
    password,
  });
  // Also keep the token in memory so same-tab API calls can send an
  // Authorization header. Persistence is the HttpOnly cookie's job.
  setAccessToken(result.access_token);
  try {
    return await fetchCurrentUser();
  } catch (error) {
    setAccessToken(null);
    throw error;
  }
}

export async function logout(): Promise<void> {
  try {
    await apiPostJson("/api/auth/logout");
  } finally {
    setAccessToken(null);
  }
}
