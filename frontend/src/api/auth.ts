import { apiGet, apiPostJson, ApiRequestError, setAccessToken } from "./client";

export interface AuthUser {
  id: number;
  user_id: number;
  username: string;
  role: string;
}

export interface AuthIdentity {
  user: AuthUser;
}

export interface AuthStatus {
  initialized: boolean;
}

export interface AuthSession extends AuthStatus {
  user: AuthUser | null;
}

interface LoginResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
}

interface MeResponse {
  id: number;
  user_id: number;
  username: string;
  role: string;
}

export function fetchAuthStatus(): Promise<AuthStatus> {
  return apiGet<AuthStatus>("/api/auth/status");
}

export async function fetchCurrentUser(): Promise<AuthIdentity> {
  const response = await apiGet<MeResponse>("/api/auth/me");
  return {
    user: {
      id: response.id,
      user_id: response.user_id ?? response.id,
      username: response.username,
      role: response.role,
    },
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
  try {
    const identity = await fetchCurrentUser();
    return { ...status, ...identity };
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 401) {
      return { ...status, user: null };
    }
    throw error;
  }
}

export async function login(
  username: string,
  password: string,
): Promise<AuthIdentity> {
  const result = await apiPostJson<LoginResponse>("/api/auth/login", {
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

/**
 * Creates the first administrator while the instance is still uninitialized.
 * On success the backend also issues a session, so the caller is logged in.
 */
export async function register(
  username: string,
  password: string,
): Promise<AuthIdentity> {
  const result = await apiPostJson<LoginResponse>("/api/auth/register", {
    username,
    password,
  });
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
